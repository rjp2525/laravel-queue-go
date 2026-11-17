//go:build integration

package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// laravelPayload is a payload as Laravel + Horizon push it, including keys
// this package does not model (silenced, data.batchId) and an empty tags array.
const laravelPayload = `{"uuid":"0b1e7c6a-0000-4000-8000-000000000001","displayName":"App\\Jobs\\Report","job":"Illuminate\\Queue\\CallQueuedHandler@call","maxTries":3,"maxExceptions":null,"failOnTimeout":false,"backoff":null,"timeout":null,"retryUntil":null,"silenced":true,"tags":[],"data":{"commandName":"App\\Jobs\\Report","command":"O:15:\"App\\Jobs\\Report\":0:{}","batchId":"batch-1"},"id":"abc123","attempts":0}`

func TestIntegrationPopIncrementsAttempts(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()
	keys := NewKeys("test_")

	_, err := driver.Push(ctx, "default", []byte(laravelPayload))
	require.NoError(t, err)

	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw)

	assert.Equal(t, laravelPayload, raw.Body, "Body is the payload as pushed")

	// The reserved set holds the payload with attempts already incremented,
	// so the attempt is counted even if this worker dies mid-job.
	members, err := client.ZRange(ctx, keys.Reserved("default"), 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{raw.Reserved}, members)

	job, err := ParseJob(ctx, raw, driver)
	require.NoError(t, err)
	assert.Equal(t, 1, job.Attempts(), "first run is attempt 1, as in Laravel")
	assert.Empty(t, job.Tags(), `cjson turns "tags":[] into {}; it must still parse`)
}

func TestIntegrationJobReleaseMovesReservedPayload(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()
	keys := NewKeys("test_")

	_, err := driver.Push(ctx, "default", []byte(laravelPayload))
	require.NoError(t, err)

	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	job, err := ParseJob(ctx, raw, driver)
	require.NoError(t, err)

	require.NoError(t, job.Release(0))

	reserved, err := client.ZCard(ctx, keys.Reserved("default")).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), reserved, "release must remove the job from the reserved set")

	delayed, err := client.ZRange(ctx, keys.Delayed("default"), 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, delayed, 1, "exactly one copy of the job may exist")

	var released struct {
		Attempts int  `json:"attempts"`
		Silenced bool `json:"silenced"`
		Data     struct {
			BatchID string `json:"batchId"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(delayed[0]), &released))
	assert.Equal(t, 1, released.Attempts)
	assert.True(t, released.Silenced, "payload keys unknown to this package must survive a release")
	assert.Equal(t, "batch-1", released.Data.BatchID)

	// Second run sees attempt 2.
	require.NoError(t, driver.MigrateExpiredJobs(ctx, "default"))
	raw2, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw2)
	job2, err := ParseJob(ctx, raw2, driver)
	require.NoError(t, err)
	assert.Equal(t, 2, job2.Attempts())

	require.NoError(t, job2.Delete())
	reserved, err = client.ZCard(ctx, keys.Reserved("default")).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), reserved)
}

func TestIntegrationNotifyListDoesNotGrow(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client) // BlockFor is zero
	ctx := context.Background()
	keys := NewKeys("test_")

	for range 5 {
		_, err := driver.Push(ctx, "default", testPayload(t, `App\Jobs\Notify`))
		require.NoError(t, err)
		raw, err := driver.Pop(ctx, "default")
		require.NoError(t, err)
		require.NotNil(t, raw)
		require.NoError(t, driver.Delete(ctx, raw))
	}

	n, err := client.LLen(ctx, keys.Notify("default")).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "each pop consumes the notification its push created")
}

func TestIntegrationLaterDoesNotNotifyUntilMigrated(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()
	keys := NewKeys("test_")

	_, err := driver.Later(ctx, "default", time.Hour, testPayload(t, `App\Jobs\Delayed`))
	require.NoError(t, err)

	n, err := client.LLen(ctx, keys.Notify("default")).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "a delayed job is not ready, so nothing should wake a blocked worker")
}

func TestIntegrationBlockingPopTakesReadyJobImmediately(t *testing.T) {
	client := setupRedis(t)
	keys := NewKeys("test_")
	ctx := context.Background()

	driver := NewRedisDriver(client, RedisDriverConfig{
		Prefix:     "test_",
		RetryAfter: 60 * time.Second,
		BlockFor:   5 * time.Second,
	})

	// A ready job with no pending notification, e.g. the notification was
	// consumed by another worker's BLPOP.
	require.NoError(t, client.RPush(ctx, keys.Ready("default"), string(testPayload(t, `App\Jobs\Ready`))).Err())

	start := time.Now()
	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw)
	assert.Less(t, time.Since(start), 2*time.Second, "must not wait out BlockFor when a job is ready")
}

func TestIntegrationBlockingPopWakesOnPush(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()

	driver := NewRedisDriver(client, RedisDriverConfig{
		Prefix:     "test_",
		RetryAfter: 60 * time.Second,
		BlockFor:   10 * time.Second,
	})

	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = driver.Push(ctx, "default", testPayload(t, `App\Jobs\Wake`))
	}()

	start := time.Now()
	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestIntegrationWorkerRunsJobExactlyMaxTriesTimes(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	keys := NewKeys("test_")

	_, err := driver.Push(ctx, "default", []byte(laravelPayload)) // maxTries: 3
	require.NoError(t, err)

	var runs []int
	worker := NewWorker(driver, WorkerOptions{Queue: "default", Sleep: 100 * time.Millisecond})
	worker.Register(`App\Jobs\Report`, func(_ context.Context, job *Job) error {
		runs = append(runs, job.Attempts())
		return assert.AnError
	})
	worker.On(events.JobFailed, func(events.Event) { worker.Stop() })

	require.NoError(t, worker.Run(ctx))
	assert.Equal(t, []int{1, 2, 3}, runs)

	for _, key := range []string{keys.Reserved("default"), keys.Delayed("default")} {
		n, err := client.ZCard(ctx, key).Result()
		require.NoError(t, err)
		assert.Equal(t, int64(0), n, key)
	}
}
