package queue

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisDriver struct {
	client     redis.Cmdable
	version    LaravelVersion
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

	// LaravelVersion is the major version of the Laravel app sharing the
	// queue. It selects the default Prefix (when Prefix is empty) and the
	// payload format dispatchers write, and on a *redis.ClusterClient it
	// turns on Laravel 13's hash-tagged keys. Leave it as LaravelAuto to
	// detect the version from jobs already on the queue.
	LaravelVersion LaravelVersion
	// HashTagQueues forces hash-tagged queue keys ("queues:{name}"), for a
	// Laravel 13 app on Redis Cluster when LaravelVersion is left on auto.
	HashTagQueues bool
}

func NewRedisDriver(client redis.Cmdable, cfg RedisDriverConfig) *RedisDriver {
	if cfg.NoPrefix {
		cfg.Prefix = ""
	} else if cfg.Prefix == "" {
		cfg.Prefix = cfg.LaravelVersion.RedisPrefix("")
	}
	if cfg.RetryAfter == 0 {
		cfg.RetryAfter = 90 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	keys := NewKeys(cfg.Prefix)
	if _, cluster := client.(*redis.ClusterClient); cluster && cfg.LaravelVersion >= Laravel13 {
		keys.HashTag = true
	}
	if cfg.HashTagQueues {
		keys.HashTag = true
	}

	return &RedisDriver{
		client:         client,
		version:        cfg.LaravelVersion,
		keys:           keys,
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

// DetectLaravelVersion returns the configured LaravelVersion, or, when that
// is LaravelAuto, the version inferred from the first job found on the queue
// (ready, delayed or reserved). It returns LaravelAuto when the queue holds
// no job to look at. See the package-level DetectLaravelVersion for how
// reliable the inference is.
func (d *RedisDriver) DetectLaravelVersion(ctx context.Context, queue string) (LaravelVersion, error) {
	if d.version != LaravelAuto {
		return d.version, nil
	}

	peek := []func() (string, error){
		func() (string, error) { return d.client.LIndex(ctx, d.keys.Ready(queue), 0).Result() },
		func() (string, error) { return firstMember(d.client.ZRange(ctx, d.keys.Delayed(queue), 0, 0).Result()) },
		func() (string, error) {
			return firstMember(d.client.ZRange(ctx, d.keys.Reserved(queue), 0, 0).Result())
		},
	}
	for _, next := range peek {
		payload, err := next()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return LaravelAuto, fmt.Errorf("detect laravel version on %s: %w", queue, err)
		}
		if v, ok := DetectLaravelVersion([]byte(payload)); ok {
			return v, nil
		}
	}
	return LaravelAuto, nil
}

func firstMember(members []string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if len(members) == 0 {
		return "", redis.Nil
	}
	return members[0], nil
}

// QueueLocation is a queue found in Redis by DiscoverQueues.
type QueueLocation struct {
	Prefix string // key prefix, e.g. "laravel_database_"
	Queue  string // queue name, e.g. "default"
}

// DiscoverQueues scans Redis for Laravel queue keys and reports the prefix
// and name of every queue that currently holds jobs. Use it to find out which
// prefix an application really uses; an empty queue has no keys and is not
// found.
//
// It walks the keyspace with SCAN, which is safe on a live server but takes
// time proportional to the number of keys. On a *redis.ClusterClient every
// master is scanned.
func DiscoverQueues(ctx context.Context, client redis.Cmdable) ([]QueueLocation, error) {
	seen := make(map[QueueLocation]struct{})

	scan := func(ctx context.Context, c redis.Cmdable) error {
		iter := c.Scan(ctx, 0, "*queues:*", 1000).Iterator()
		for iter.Next(ctx) {
			if loc, ok := parseQueueKey(iter.Val()); ok {
				seen[loc] = struct{}{}
			}
		}
		return iter.Err()
	}

	var err error
	if cluster, ok := client.(*redis.ClusterClient); ok {
		var mu sync.Mutex
		err = cluster.ForEachMaster(ctx, func(ctx context.Context, node *redis.Client) error {
			mu.Lock()
			defer mu.Unlock()
			return scan(ctx, node)
		})
	} else {
		err = scan(ctx, client)
	}
	if err != nil {
		return nil, fmt.Errorf("discover queues: %w", err)
	}

	return slices.SortedFunc(maps.Keys(seen), func(a, b QueueLocation) int {
		return cmp.Or(cmp.Compare(a.Prefix, b.Prefix), cmp.Compare(a.Queue, b.Queue))
	}), nil
}

// parseQueueKey splits "{prefix}queues:{name}[:delayed|:reserved|:notify]".
func parseQueueKey(key string) (QueueLocation, bool) {
	prefix, name, found := strings.Cut(key, "queues:")
	if !found || name == "" {
		return QueueLocation{}, false
	}
	for _, suffix := range []string{":delayed", ":reserved", ":notify"} {
		name = strings.TrimSuffix(name, suffix)
	}
	// Laravel 13 on Redis Cluster stores "queues:{name}".
	if len(name) > 2 && name[0] == '{' && name[len(name)-1] == '}' {
		name = name[1 : len(name)-1]
	}
	return QueueLocation{Prefix: prefix, Queue: name}, true
}

func (d *RedisDriver) Close() error {
	return nil
}

var _ Driver = (*RedisDriver)(nil)
