package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisDriver struct {
	client     redis.Cmdable
	keys       *Keys
	retryAfter time.Duration
	blockFor   time.Duration
	batchSize  int

	// Throttle migration to avoid 2 extra Redis round-trips on every Pop.
	migrateMu      sync.Mutex
	lastMigratedAt map[string]time.Time
}

type RedisDriverConfig struct {
	Prefix     string
	NoPrefix   bool // When true, use empty prefix regardless of Prefix value.
	RetryAfter time.Duration
	BlockFor   time.Duration
	BatchSize  int
}

func NewRedisDriver(client redis.Cmdable, cfg RedisDriverConfig) *RedisDriver {
	if cfg.NoPrefix {
		cfg.Prefix = ""
	} else if cfg.Prefix == "" {
		cfg.Prefix = DefaultRedisPrefix
	}
	if cfg.RetryAfter == 0 {
		cfg.RetryAfter = 90 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	return &RedisDriver{
		client:         client,
		keys:           NewKeys(cfg.Prefix),
		retryAfter:     cfg.RetryAfter,
		blockFor:       cfg.BlockFor,
		batchSize:      cfg.BatchSize,
		lastMigratedAt: make(map[string]time.Time),
	}
}

func (d *RedisDriver) Push(ctx context.Context, queue string, payload []byte) (string, error) {
	err := luaPush.Run(ctx, d.client,
		[]string{d.keys.Ready(queue), d.keys.Notify(queue)},
		string(payload),
	).Err()
	if err != nil {
		return "", fmt.Errorf("push to %s: %w", queue, err)
	}
	return "", nil
}

func (d *RedisDriver) Later(ctx context.Context, queue string, delay time.Duration, payload []byte) (string, error) {
	availableAt := float64(time.Now().Add(delay).Unix())
	err := luaLater.Run(ctx, d.client,
		[]string{d.keys.Delayed(queue)},
		availableAt, string(payload),
	).Err()
	if err != nil {
		return "", fmt.Errorf("later to %s: %w", queue, err)
	}
	return "", nil
}

func (d *RedisDriver) Pop(ctx context.Context, queue string) (*RawJob, error) {
	// Throttle migration to at most once per second per queue.
	if d.shouldMigrate(queue) {
		if err := d.MigrateExpiredJobs(ctx, queue); err != nil {
			return nil, fmt.Errorf("migrate expired jobs: %w", err)
		}
	}

	// Same order as Laravel's RedisQueue::retrieveNextJob: take a job if one
	// is ready, and only block on the notify list when the queue is empty.
	job, err := d.popFromQueue(ctx, queue)
	if err != nil || job != nil || d.blockFor <= 0 {
		return job, err
	}

	if err := d.client.BLPop(ctx, d.blockFor, d.keys.Notify(queue)).Err(); err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // timed out with nothing pushed
		}
		return nil, fmt.Errorf("wait for job on %s: %w", queue, err)
	}

	return d.popFromQueue(ctx, queue)
}

func (d *RedisDriver) shouldMigrate(queue string) bool {
	d.migrateMu.Lock()
	defer d.migrateMu.Unlock()
	last := d.lastMigratedAt[queue]
	if time.Since(last) < time.Second {
		return false
	}
	d.lastMigratedAt[queue] = time.Now()
	return true
}

func (d *RedisDriver) popFromQueue(ctx context.Context, queue string) (*RawJob, error) {
	reservedUntil := time.Now().Add(d.retryAfter)

	result, err := luaPop.Run(ctx, d.client,
		[]string{d.keys.Ready(queue), d.keys.Reserved(queue), d.keys.Notify(queue)},
		float64(reservedUntil.Unix()),
	).Slice()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pop from %s: %w", queue, err)
	}

	// The script returns {job, reserved}; an empty queue yields Lua false for
	// both, which arrives as nil (RESP2) or false (RESP3).
	if len(result) < 2 {
		return nil, nil
	}
	body, ok := result[0].(string)
	if !ok {
		return nil, nil
	}
	reserved, ok := result[1].(string)
	if !ok {
		return nil, nil
	}

	return &RawJob{
		Body:          body,
		Reserved:      reserved,
		Queue:         queue,
		ReservedUntil: reservedUntil,
	}, nil
}

// Delete removes a job from the reserved set (ACK).
func (d *RedisDriver) Delete(ctx context.Context, job *RawJob) error {
	err := luaDelete.Run(ctx, d.client,
		[]string{d.keys.Reserved(job.Queue)},
		job.reserved(),
	).Err()
	if err != nil {
		return fmt.Errorf("delete from reserved: %w", err)
	}
	return nil
}

// Release moves a job back to the delayed queue with a delay (NACK).
func (d *RedisDriver) Release(ctx context.Context, job *RawJob, delay time.Duration) error {
	availableAt := float64(time.Now().Add(delay).Unix())
	err := luaRelease.Run(ctx, d.client,
		[]string{d.keys.Delayed(job.Queue), d.keys.Reserved(job.Queue)},
		job.reserved(), availableAt,
	).Err()
	if err != nil {
		return fmt.Errorf("release job: %w", err)
	}
	return nil
}

// Size is the number of jobs on the queue in any state: ready, delayed and
// reserved. It is the same number Laravel's Queue::size() reports. Use
// PendingSize for the jobs that are ready to run now.
func (d *RedisDriver) Size(ctx context.Context, queue string) (int64, error) {
	size, err := luaSize.Run(ctx, d.client,
		[]string{d.keys.Ready(queue), d.keys.Delayed(queue), d.keys.Reserved(queue)},
	).Int64()
	if err != nil {
		return 0, fmt.Errorf("size of %s: %w", queue, err)
	}
	return size, nil
}

// PendingSize is the number of jobs ready to be reserved, like Laravel 12's
// Queue::pendingSize().
func (d *RedisDriver) PendingSize(ctx context.Context, queue string) (int64, error) {
	return d.client.LLen(ctx, d.keys.Ready(queue)).Result()
}

// DelayedSize is the number of jobs waiting for their delay or retry backoff
// to pass, like Laravel 12's Queue::delayedSize().
func (d *RedisDriver) DelayedSize(ctx context.Context, queue string) (int64, error) {
	return d.client.ZCard(ctx, d.keys.Delayed(queue)).Result()
}

// ReservedSize is the number of jobs currently held by workers, like Laravel
// 12's Queue::reservedSize().
func (d *RedisDriver) ReservedSize(ctx context.Context, queue string) (int64, error) {
	return d.client.ZCard(ctx, d.keys.Reserved(queue)).Result()
}

func (d *RedisDriver) Clear(ctx context.Context, queue string) (int64, error) {
	result, err := luaClear.Run(ctx, d.client,
		[]string{
			d.keys.Ready(queue),
			d.keys.Delayed(queue),
			d.keys.Reserved(queue),
			d.keys.Notify(queue),
		},
	).Int64()
	if err != nil {
		return 0, fmt.Errorf("clear queue %s: %w", queue, err)
	}
	return result, nil
}

func (d *RedisDriver) MigrateExpiredJobs(ctx context.Context, queue string) error {
	now := float64(time.Now().Unix())

	if err := d.migrateFrom(ctx, d.keys.Delayed(queue), d.keys.Ready(queue), d.keys.Notify(queue), now); err != nil {
		return fmt.Errorf("migrate delayed: %w", err)
	}

	if err := d.migrateFrom(ctx, d.keys.Reserved(queue), d.keys.Ready(queue), d.keys.Notify(queue), now); err != nil {
		return fmt.Errorf("migrate reserved: %w", err)
	}

	return nil
}

func (d *RedisDriver) migrateFrom(ctx context.Context, from, to, notify string, now float64) error {
	return luaMigrate.Run(ctx, d.client,
		[]string{from, to, notify},
		now, d.batchSize,
	).Err()
}

func (d *RedisDriver) Close() error {
	return nil
}

var _ Driver = (*RedisDriver)(nil)
