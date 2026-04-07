package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfigValues(t *testing.T) {
	c := DefaultConfig()
	require.NotNil(t, c)

	assert.Equal(t, "127.0.0.1:6379", c.RedisAddr)
	assert.Equal(t, "", c.RedisPassword)
	assert.Equal(t, 0, c.RedisDB)
	assert.Equal(t, "laravel-database-", c.RedisPrefix)
	assert.Equal(t, "default", c.DefaultQueue)
	assert.Equal(t, 90*time.Second, c.RetryAfter)
	assert.Equal(t, 5*time.Second, c.BlockFor)
	assert.Equal(t, 1, c.Concurrency)
	assert.Equal(t, 0, c.MaxJobs)
	assert.Equal(t, 0, c.MaxTime)
	assert.Equal(t, 3*time.Second, c.Sleep)
	assert.Equal(t, 100, c.MigrationBatchSize)
	assert.Equal(t, "failed_jobs", c.FailedJobsTable)
	assert.Equal(t, "database", c.FailedJobsDriver)
	assert.Equal(t, "", c.DatabaseDSN)
}

// ---------------------------------------------------------------------------
// FromEnv
// ---------------------------------------------------------------------------

func TestFromEnvDefaults(t *testing.T) {
	// Without any env vars set, FromEnv should return defaults.
	c := FromEnv()
	assert.Equal(t, "127.0.0.1:6379", c.RedisAddr)
	assert.Equal(t, "default", c.DefaultQueue)
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("REDIS_HOST", "redis.example.com:6380")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "3")
	t.Setenv("REDIS_PREFIX", "myapp_")
	t.Setenv("QUEUE_NAME", "high")
	t.Setenv("QUEUE_RETRY_AFTER", "120s")
	t.Setenv("QUEUE_BLOCK_FOR", "10s")
	t.Setenv("QUEUE_CONCURRENCY", "4")
	t.Setenv("QUEUE_MAX_JOBS", "1000")
	t.Setenv("QUEUE_MAX_TIME", "3600")
	t.Setenv("QUEUE_SLEEP", "1s")
	t.Setenv("QUEUE_MIGRATION_BATCH_SIZE", "50")
	t.Setenv("QUEUE_FAILED_TABLE", "my_failed_jobs")
	t.Setenv("QUEUE_FAILED_DRIVER", "file")
	t.Setenv("DATABASE_URL", "postgres://localhost/mydb")

	c := FromEnv()

	assert.Equal(t, "redis.example.com:6380", c.RedisAddr)
	assert.Equal(t, "secret", c.RedisPassword)
	assert.Equal(t, 3, c.RedisDB)
	assert.Equal(t, "myapp_", c.RedisPrefix)
	assert.Equal(t, "high", c.DefaultQueue)
	assert.Equal(t, 120*time.Second, c.RetryAfter)
	assert.Equal(t, 10*time.Second, c.BlockFor)
	assert.Equal(t, 4, c.Concurrency)
	assert.Equal(t, 1000, c.MaxJobs)
	assert.Equal(t, 3600, c.MaxTime)
	assert.Equal(t, 1*time.Second, c.Sleep)
	assert.Equal(t, 50, c.MigrationBatchSize)
	assert.Equal(t, "my_failed_jobs", c.FailedJobsTable)
	assert.Equal(t, "file", c.FailedJobsDriver)
	assert.Equal(t, "postgres://localhost/mydb", c.DatabaseDSN)
}

func TestFromEnvInvalidNumbersFallbackToDefaults(t *testing.T) {
	t.Setenv("REDIS_DB", "not-a-number")
	t.Setenv("QUEUE_CONCURRENCY", "abc")
	t.Setenv("QUEUE_RETRY_AFTER", "invalid")

	c := FromEnv()

	assert.Equal(t, 0, c.RedisDB)
	assert.Equal(t, 1, c.Concurrency)
	assert.Equal(t, 90*time.Second, c.RetryAfter)
}

func TestFromEnvPartialOverride(t *testing.T) {
	t.Setenv("QUEUE_NAME", "emails")

	c := FromEnv()

	assert.Equal(t, "emails", c.DefaultQueue)
	// Everything else should be default.
	assert.Equal(t, "127.0.0.1:6379", c.RedisAddr)
	assert.Equal(t, 1, c.Concurrency)
}
