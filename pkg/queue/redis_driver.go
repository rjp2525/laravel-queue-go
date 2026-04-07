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
	RetryAfter time.Duration
	BlockFor   time.Duration
	BatchSize  int
}

func NewRedisDriver(client redis.Cmdable, cfg RedisDriverConfig) *RedisDriver {
	if cfg.Prefix == "" {
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
		[]string{d.keys.Delayed(queue), d.keys.Notify(queue)},
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

	// Try blocking pop on the notify queue if blockFor is set.
	if d.blockFor > 0 {
		_, err := d.client.BLPop(ctx, d.blockFor, d.keys.Notify(queue)).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			// Non-fatal — fall through to direct pop.
		}
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
	reservedUntil := float64(time.Now().Add(d.retryAfter).Unix())

	result, err := luaPop.Run(ctx, d.client,
		[]string{d.keys.Ready(queue), d.keys.Reserved(queue)},
		reservedUntil,
	).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pop: %w", err)
	}

	body, ok := result.(string)
	if !ok {
		return nil, nil
	}

	return &RawJob{
		Body:          body,
		Queue:         queue,
		ReservedUntil: time.Now().Add(d.retryAfter),
	}, nil
}

// Delete removes a job from the reserved set (ACK).
func (d *RedisDriver) Delete(ctx context.Context, job *RawJob) error {
	err := luaDelete.Run(ctx, d.client,
		[]string{d.keys.Reserved(job.Queue)},
		job.Body,
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
		job.Body, availableAt,
	).Err()
	if err != nil {
		return fmt.Errorf("release job: %w", err)
	}
	return nil
}

func (d *RedisDriver) Size(ctx context.Context, queue string) (int64, error) {
	return d.client.LLen(ctx, d.keys.Ready(queue)).Result()
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
