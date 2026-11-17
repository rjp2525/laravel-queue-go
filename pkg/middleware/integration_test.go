//go:build integration

package middleware

import (
	"context"
	"errors"
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
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: endpoint})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func byName(job Job) string { return job.DisplayName() }

func TestIntegrationLockIsNotReleasedOnceOwnedBySomeoneElse(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	const key = overlappingKeyPrefix + "TestJob"

	mw := WithoutOverlapping(client, byName, 200*time.Millisecond)
	err := mw(ctx, stubJob{}, func() error {
		// The job outlives the lock's TTL and another worker takes the lock.
		time.Sleep(400 * time.Millisecond)
		ok, err := client.SetNX(ctx, key, "another-worker", time.Minute).Result()
		require.NoError(t, err)
		require.True(t, ok, "the lock expired, so it is free to take")
		return nil
	})
	require.NoError(t, err)

	owner, err := client.Get(ctx, key).Result()
	require.NoError(t, err, "the other worker's lock must survive")
	assert.Equal(t, "another-worker", owner)
}

func TestIntegrationLockIsReleasedAfterJob(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	const key = uniqueKeyPrefix + "TestJob"

	mw := Unique(client, byName, time.Minute)

	var held bool
	require.NoError(t, mw(ctx, stubJob{}, func() error {
		n, err := client.Exists(ctx, key).Result()
		require.NoError(t, err)
		held = n == 1
		return nil
	}))
	assert.True(t, held)

	n, err := client.Exists(ctx, key).Result()
	require.NoError(t, err)
	assert.Zero(t, n)

	// Also after a failing job.
	boom := errors.New("boom")
	assert.ErrorIs(t, mw(ctx, stubJob{}, func() error { return boom }), boom)
	n, err = client.Exists(ctx, key).Result()
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestIntegrationLockIsReleasedWhenContextIsCancelledDuringJob(t *testing.T) {
	client := setupRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const key = overlappingKeyPrefix + "TestJob"

	mw := WithoutOverlapping(client, byName, 0) // no TTL: a leaked lock would be permanent
	require.NoError(t, mw(ctx, stubJob{}, func() error {
		cancel() // worker shutdown while the job runs
		return nil
	}))

	n, err := client.Exists(context.Background(), key).Result()
	require.NoError(t, err)
	assert.Zero(t, n, "the lock must not be left behind")
}

func TestIntegrationSecondJobIsTurnedAwayWhileLockIsHeld(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	overlap := WithoutOverlapping(client, byName, time.Minute)
	unique := Unique(client, byName, time.Minute)

	require.NoError(t, overlap(ctx, stubJob{}, func() error {
		err := overlap(ctx, stubJob{}, func() error { t.Fatal("must not run"); return nil })
		var release *ReleaseError
		assert.ErrorAs(t, err, &release)
		return nil
	}))

	require.NoError(t, unique(ctx, stubJob{}, func() error {
		assert.NoError(t, unique(ctx, stubJob{}, func() error { t.Fatal("must not run"); return nil }))
		return nil
	}))
}

func TestIntegrationRateLimitWithSubSecondWindow(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	mw := RateLimited(client, "api", 1, 500*time.Millisecond)
	ran := 0
	next := func() error { ran++; return nil }

	require.NoError(t, mw(ctx, stubJob{}, next))

	err := mw(ctx, stubJob{}, next)
	var release *ReleaseError
	require.ErrorAs(t, err, &release, "the second job inside the window is released")
	assert.Equal(t, 1, ran)

	ttl, err := client.PTTL(ctx, rateLimitKeyPrefix+"api").Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0))
	assert.LessOrEqual(t, ttl, 500*time.Millisecond)

	time.Sleep(600 * time.Millisecond)
	require.NoError(t, mw(ctx, stubJob{}, next), "the window has passed")
	assert.Equal(t, 2, ran)
}
