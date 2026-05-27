//go:build integration

package queue

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func setupRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { container.Terminate(ctx) })
	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: endpoint})
	t.Cleanup(func() { client.Close() })
	return client
}

func newTestDriver(client *redis.Client) *RedisDriver {
	return NewRedisDriver(client, RedisDriverConfig{
		Prefix:     "test_",
		RetryAfter: 60 * time.Second,
		BatchSize:  100,
	})
}

func testPayload(t *testing.T, name string) []byte {
	t.Helper()
	p := NewPayload(name, `O:4:"Test":0:{}`)
	data, err := json.Marshal(p)
	require.NoError(t, err)
	return data
}

// ---------------------------------------------------------------------------
// Push / Pop round-trip
// ---------------------------------------------------------------------------

func TestIntegrationPushPop(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	payload := testPayload(t, `App\Jobs\TestJob`)
	_, err := driver.Push(ctx, "default", payload)
	require.NoError(t, err)

	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw)

	var got LaravelPayload
	err = json.Unmarshal([]byte(raw.Body), &got)
	require.NoError(t, err)
	assert.Equal(t, `App\Jobs\TestJob`, got.DisplayName)
}

// ---------------------------------------------------------------------------
// Later / Pop with zero delay
// ---------------------------------------------------------------------------

func TestIntegrationLaterPop(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	payload := testPayload(t, `App\Jobs\DelayedJob`)
	_, err := driver.Later(ctx, "default", 0, payload)
	require.NoError(t, err)

	// With 0 delay, the job should be available after migration.
	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw)

	var got LaravelPayload
	err = json.Unmarshal([]byte(raw.Body), &got)
	require.NoError(t, err)
	assert.Equal(t, `App\Jobs\DelayedJob`, got.DisplayName)
}

// ---------------------------------------------------------------------------
// Delete (ACK)
// ---------------------------------------------------------------------------

func TestIntegrationDelete(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	payload := testPayload(t, `App\Jobs\AckJob`)
	_, err := driver.Push(ctx, "default", payload)
	require.NoError(t, err)

	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw)

	err = driver.Delete(ctx, raw)
	require.NoError(t, err)

	// Reserved set should be empty.
	keys := NewKeys("test_")
	count, err := client.ZCard(ctx, keys.Reserved("default")).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

// ---------------------------------------------------------------------------
// Release (NACK back to delayed)
// ---------------------------------------------------------------------------

func TestIntegrationRelease(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	payload := testPayload(t, `App\Jobs\NackJob`)
	_, err := driver.Push(ctx, "default", payload)
	require.NoError(t, err)

	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw)

	// Release with zero delay so it can be immediately migrated.
	err = driver.Release(ctx, raw, 0)
	require.NoError(t, err)

	// Should be in delayed set now.
	keys := NewKeys("test_")
	count, err := client.ZCard(ctx, keys.Delayed("default")).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Force migration (Pop throttles migration to once/sec, so call it directly).
	err = driver.MigrateExpiredJobs(ctx, "default")
	require.NoError(t, err)

	raw2, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, raw2)
}

// ---------------------------------------------------------------------------
// Clear
// ---------------------------------------------------------------------------

func TestIntegrationClear(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := driver.Push(ctx, "default", testPayload(t, `App\Jobs\Bulk`))
		require.NoError(t, err)
	}

	cleared, err := driver.Clear(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(5), cleared)

	size, err := driver.Size(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

// ---------------------------------------------------------------------------
// Size
// ---------------------------------------------------------------------------

func TestIntegrationSize(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	size, err := driver.Size(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(0), size)

	_, err = driver.Push(ctx, "default", testPayload(t, "Job"))
	require.NoError(t, err)

	size, err = driver.Size(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(1), size)
}

// ---------------------------------------------------------------------------
// MigrateExpiredJobs
// ---------------------------------------------------------------------------

func TestIntegrationMigrateExpiredJobs(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()
	keys := NewKeys("test_")

	// Add a job to delayed with a past timestamp so it's expired.
	payload := testPayload(t, `App\Jobs\Expired`)
	pastScore := float64(time.Now().Unix() - 100)
	err := client.ZAdd(ctx, keys.Delayed("default"), redis.Z{
		Score:  pastScore,
		Member: string(payload),
	}).Err()
	require.NoError(t, err)

	err = driver.MigrateExpiredJobs(ctx, "default")
	require.NoError(t, err)

	// Should now be in the ready queue.
	size, err := driver.Size(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(1), size)
}

// ---------------------------------------------------------------------------
// Pop returns nil when empty
// ---------------------------------------------------------------------------

func TestIntegrationPopEmpty(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	raw, err := driver.Pop(ctx, "default")
	require.NoError(t, err)
	assert.Nil(t, raw)
}

// ---------------------------------------------------------------------------
// Close is a no-op
// ---------------------------------------------------------------------------

func TestIntegrationClose(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	assert.NoError(t, driver.Close())
}

// ---------------------------------------------------------------------------
// Full dispatch -> worker consume flow
// ---------------------------------------------------------------------------

// TestIntegrationGracefulStop verifies that Stop() lets in-flight handlers
// finish on the parent context (no cancellation) and that Run() blocks until
// they drain. Regression test for the SIGTERM-mid-job behavior that left
// reserved jobs stuck waiting for retry_after.
func TestIntegrationGracefulStop(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	dispatcher := NewDispatcher(driver, WithDefaultQueue("graceful"))
	err := dispatcher.Dispatch(ctx, `App\Jobs\Slow`, nil)
	require.NoError(t, err)

	worker := NewWorker(driver, WorkerOptions{
		Queue:       "graceful",
		Concurrency: 2,
		Sleep:       50 * time.Millisecond,
	})

	handlerStarted := make(chan struct{})
	var completedNormally atomic.Bool
	worker.Register(`App\Jobs\Slow`, func(ctx context.Context, job *Job) error {
		close(handlerStarted)
		// Sleep longer than the time we'll wait before calling Stop. If Stop
		// cancelled the context, this would return early via ctx.Err().
		select {
		case <-time.After(500 * time.Millisecond):
			completedNormally.Store(true)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	runErr := make(chan error, 1)
	go func() { runErr <- worker.Run(ctx) }()

	// Wait for the handler to be in flight, then ask the worker to stop.
	<-handlerStarted
	stopAt := time.Now()
	worker.Stop()

	select {
	case err := <-runErr:
		drainDuration := time.Since(stopAt)
		assert.NoError(t, err, "Run should return cleanly after graceful Stop")
		assert.True(t, completedNormally.Load(),
			"handler should complete normally, not via context cancellation")
		// Drain must have waited for the ~500ms handler — at least 300ms gives
		// generous slack for scheduling jitter.
		assert.GreaterOrEqual(t, drainDuration, 300*time.Millisecond,
			"Run should block until in-flight handlers finish")
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Stop within 2s")
	}
}

func TestIntegrationDispatchAndConsume(t *testing.T) {
	client := setupRedis(t)
	driver := newTestDriver(client)
	ctx := context.Background()

	// Dispatch a job.
	dispatcher := NewDispatcher(driver, WithDefaultQueue("jobs"))
	err := dispatcher.Dispatch(ctx, `App\Jobs\TestConsume`, map[string]any{
		"message": "hello",
	})
	require.NoError(t, err)

	// Set up a worker that stops when the queue is empty.
	var called atomic.Int32
	worker := NewWorker(driver, WorkerOptions{
		Queue:       "jobs",
		StopOnEmpty: true,
		Sleep:       100 * time.Millisecond,
	})
	worker.Register(`App\Jobs\TestConsume`, func(ctx context.Context, job *Job) error {
		called.Add(1)
		assert.Equal(t, `App\Jobs\TestConsume`, job.CommandName())
		assert.Equal(t, "hello", job.GetString("message"))
		return nil
	})

	err = worker.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(1), called.Load())
}
