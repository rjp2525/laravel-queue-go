package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Sharing a Laravel .env
// ---------------------------------------------------------------------------

func TestFromEnvLaravelDotEnv(t *testing.T) {
	// The Redis block of a stock Laravel .env.
	t.Setenv("REDIS_CLIENT", "phpredis")
	t.Setenv("REDIS_HOST", "127.0.0.1")
	t.Setenv("REDIS_PASSWORD", "null")
	t.Setenv("REDIS_PORT", "6380")
	t.Setenv("REDIS_QUEUE", "emails")
	t.Setenv("REDIS_QUEUE_RETRY_AFTER", "120")

	c, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:6380", c.RedisAddr)
	assert.Empty(t, c.RedisPassword, `"null" means no password, as in Laravel's env()`)
	assert.Equal(t, "emails", c.DefaultQueue)
	assert.Equal(t, 120*time.Second, c.RetryAfter)
}

func TestFromEnvRedisAddr(t *testing.T) {
	for name, tc := range map[string]struct{ host, port, want string }{
		"host only":          {"redis.internal", "", "redis.internal:6379"},
		"host and port":      {"redis.internal", "6380", "redis.internal:6380"},
		"host with port":     {"redis.internal:7000", "", "redis.internal:7000"},
		"host:port wins":     {"redis.internal:7000", "6380", "redis.internal:7000"},
		"port only":          {"", "6380", "127.0.0.1:6380"},
		"ipv6":               {"::1", "6380", "[::1]:6380"},
		"bracketed ipv6":     {"[::1]", "", "[::1]:6379"},
		"ipv6 with port":     {"[::1]:7000", "", "[::1]:7000"},
		"nothing set":        {"", "", "127.0.0.1:6379"},
		"quoted, as in .env": {`"redis.internal"`, `"6380"`, "redis.internal:6380"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("REDIS_HOST", tc.host)
			t.Setenv("REDIS_PORT", tc.port)
			assert.Equal(t, tc.want, FromEnv().RedisAddr)
		})
	}
}

func TestFromEnvOwnVariablesWinOverLaravelOnes(t *testing.T) {
	t.Setenv("REDIS_QUEUE", "from-laravel")
	t.Setenv("QUEUE_NAME", "from-go")
	t.Setenv("REDIS_QUEUE_RETRY_AFTER", "120")
	t.Setenv("QUEUE_RETRY_AFTER", "45s")
	t.Setenv("REDIS_USERNAME", "worker")

	c := FromEnv()
	assert.Equal(t, "from-go", c.DefaultQueue)
	assert.Equal(t, 45*time.Second, c.RetryAfter)
	assert.Equal(t, "worker", c.RedisUsername)
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"90": 90 * time.Second, "90s": 90 * time.Second, "1m30s": 90 * time.Second,
		"0.5": 500 * time.Millisecond, "250ms": 250 * time.Millisecond, "0": 0, " 3 ": 3 * time.Second,
	} {
		got, err := ParseDuration(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "soon", "-5", "-5s", "5 minutes"} {
		_, err := ParseDuration(in)
		assert.Error(t, err, in)
	}
}

func TestLoadReportsInvalidValuesAndKeepsDefaults(t *testing.T) {
	t.Setenv("QUEUE_CONCURRENCY", "four")
	t.Setenv("QUEUE_SLEEP", "soon")
	t.Setenv("REDIS_DB", "2")

	c, err := Load()
	require.Error(t, err)
	assert.ErrorContains(t, err, `QUEUE_CONCURRENCY="four" is not an integer`)
	assert.ErrorContains(t, err, `QUEUE_SLEEP="soon" is not a duration`)

	require.NotNil(t, c)
	assert.Equal(t, 1, c.Concurrency)
	assert.Equal(t, 3*time.Second, c.Sleep)
	assert.Equal(t, 2, c.RedisDB, "valid values are still applied")

	assert.Equal(t, c, FromEnv(), "FromEnv returns the same configuration, without the error")
}
