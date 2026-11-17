//go:build integration

package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationSizeCountsEveryState(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	for range 3 {
		_, err := driver.Push(ctx, "default", testPayload(t, `App\Jobs\Ready`))
		require.NoError(t, err)
	}
	for range 2 {
		_, err := driver.Later(ctx, "default", time.Hour, testPayload(t, `App\Jobs\Delayed`))
		require.NoError(t, err)
	}
	raw, err := driver.Pop(ctx, "default") // one ready job becomes reserved
	require.NoError(t, err)
	require.NotNil(t, raw)

	sizes := map[string]func(context.Context, string) (int64, error){
		"size": driver.Size, "pending": driver.PendingSize, "delayed": driver.DelayedSize, "reserved": driver.ReservedSize,
	}
	want := map[string]int64{"size": 5, "pending": 2, "delayed": 2, "reserved": 1}
	for name, fn := range sizes {
		got, err := fn(ctx, "default")
		require.NoError(t, err, name)
		assert.Equal(t, want[name], got, name)
	}

	empty, err := driver.Size(ctx, "nothing-here")
	require.NoError(t, err)
	assert.Zero(t, empty)
}

func TestIntegrationMigrateMovesLargeBatchesInOrder(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	keys := NewKeys("test_")
	// More than one 100-job chunk, and no batch limit as in Laravel's default.
	driver := NewRedisDriver(client, RedisDriverConfig{Prefix: "test_", BatchSize: -1})

	const total = 250
	members := make([]redis.Z, total)
	for i := range total {
		// Increasing scores, all in the past: due, and ordered.
		members[i] = redis.Z{Score: float64(1000 + i), Member: fmt.Sprintf(`{"n":%d}`, i)}
	}
	require.NoError(t, client.ZAdd(ctx, keys.Delayed("default"), members...).Err())
	require.NoError(t, client.ZAdd(ctx, keys.Delayed("default"), redis.Z{
		Score: float64(time.Now().Add(time.Hour).Unix()), Member: `{"n":"future"}`,
	}).Err())

	require.NoError(t, driver.MigrateExpiredJobs(ctx, "default"))

	ready, err := client.LRange(ctx, keys.Ready("default"), 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, ready, total)
	for i, payload := range ready {
		require.Equal(t, fmt.Sprintf(`{"n":%d}`, i), payload, "jobs keep their due order")
	}

	notifications, err := client.LLen(ctx, keys.Notify("default")).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(total), notifications, "one notification per migrated job")

	delayed, err := driver.DelayedSize(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(1), delayed, "the job that is not due stays")
}

func TestIntegrationMigrateRespectsBatchSize(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	keys := NewKeys("test_")
	driver := NewRedisDriver(client, RedisDriverConfig{Prefix: "test_", BatchSize: 10})

	for i := range 25 {
		require.NoError(t, client.ZAdd(ctx, keys.Reserved("default"), redis.Z{Score: float64(1000 + i), Member: fmt.Sprintf(`{"n":%d}`, i)}).Err())
	}
	require.NoError(t, driver.MigrateExpiredJobs(ctx, "default"))

	pending, err := driver.PendingSize(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(10), pending)
	reserved, err := driver.ReservedSize(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(15), reserved)
}
