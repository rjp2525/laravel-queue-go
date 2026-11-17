//go:build integration

package queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationDetectLaravelVersion(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	driver := newTestDriver(client)
	keys := NewKeys("test_")

	v, err := driver.DetectLaravelVersion(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, LaravelAuto, v, "an empty queue gives nothing to detect")

	// Found in the ready list...
	require.NoError(t, client.RPush(ctx, keys.Ready("default"), payloadLaravel12).Err())
	v, err = driver.DetectLaravelVersion(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, Laravel12, v)

	// ...or only in the reserved set, after cjson has re-encoded it.
	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw)
	v, err = driver.DetectLaravelVersion(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, Laravel12, v)

	// A configured version is returned without looking.
	pinned := NewRedisDriver(client, RedisDriverConfig{Prefix: "test_", LaravelVersion: Laravel10})
	v, err = pinned.DetectLaravelVersion(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, Laravel10, v)
}

func TestIntegrationDispatcherAdoptsVersionOfQueuedJobs(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	driver := newTestDriver(client)
	keys := NewKeys("test_")

	require.NoError(t, client.RPush(ctx, keys.Ready("default"), payloadLaravel10).Err())
	require.NoError(t, NewDispatcher(driver).Dispatch(ctx, `App\Jobs\FromGo`, map[string]any{"n": 1}))

	pushed, err := client.LIndex(ctx, keys.Ready("default"), 1).Result()
	require.NoError(t, err)
	v, ok := DetectLaravelVersion([]byte(pushed))
	require.True(t, ok)
	assert.Equal(t, Laravel10, v)
}

func TestIntegrationDiscoverQueues(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()

	old := NewRedisDriver(client, RedisDriverConfig{LaravelVersion: Laravel11})
	_, err := old.Push(ctx, "default", []byte(payloadLaravel11))
	require.NoError(t, err)
	_, err = old.Later(ctx, "emails", 3600e9, []byte(payloadLaravel11))
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, "laravel_cache_:unrelated", "1", 0).Err())

	found, err := DiscoverQueues(ctx, client)
	require.NoError(t, err)
	assert.Equal(t, []QueueLocation{
		{Prefix: "laravel_database_", Queue: "default"},
		{Prefix: "laravel_database_", Queue: "emails"},
	}, found)
}
