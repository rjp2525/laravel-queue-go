package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Redis
	RedisAddr     string
	RedisUsername string
	RedisPassword string
	RedisDB       int
	RedisPrefix   string

	// Queue
	DefaultQueue string
	RetryAfter   time.Duration
	BlockFor     time.Duration

	// Worker
	Concurrency        int
	MaxJobs            int
	MaxTime            int
	Sleep              time.Duration
	MigrationBatchSize int

	// Failed Jobs
	FailedJobsTable  string
	FailedJobsDriver string

	// Database (for failed_jobs + batches)
	DatabaseDSN string
}

func DefaultConfig() *Config {
	return &Config{
		RedisAddr:          "127.0.0.1:6379",
		RedisDB:            0,
		RedisPrefix:        "laravel-database-",
		DefaultQueue:       "default",
		RetryAfter:         90 * time.Second,
		BlockFor:           5 * time.Second,
		Concurrency:        1,
		Sleep:              3 * time.Second,
		MigrationBatchSize: 100,
		FailedJobsTable:    "failed_jobs",
		FailedJobsDriver:   "database",
	}
}

// FromEnv loads the configuration from the environment. It understands this
// package's own variables and the ones a Laravel application's .env already
// defines, so a Go worker can share that file:
//
//   - REDIS_HOST may be "host" (combined with REDIS_PORT, default 6379) or
//     "host:port".
//   - REDIS_USERNAME and REDIS_PASSWORD; the literal "null", which Laravel's
//     .env uses for "no password", is treated as empty.
//   - REDIS_QUEUE and REDIS_QUEUE_RETRY_AFTER are used when QUEUE_NAME and
//     QUEUE_RETRY_AFTER are not set.
//   - Durations accept Go syntax ("90s", "1m30s") or a bare number of
//     seconds ("90"), which is how Laravel writes them.
//
// A variable with an invalid value keeps its default. Use Load to be told
// about those.
func FromEnv() *Config {
	c := DefaultConfig()

	if host := envValue("REDIS_HOST"); host != "" {
		c.RedisAddr = redisAddr(host, envValue("REDIS_PORT"))
	} else if port := envValue("REDIS_PORT"); port != "" {
		c.RedisAddr = redisAddr("127.0.0.1", port)
	}
	envString(&c.RedisUsername, "REDIS_USERNAME")
	envString(&c.RedisPassword, "REDIS_PASSWORD")
	_ = envInt(&c.RedisDB, "REDIS_DB")
	envString(&c.RedisPrefix, "REDIS_PREFIX")

	envString(&c.DefaultQueue, "QUEUE_NAME", "REDIS_QUEUE")
	_ = envDuration(&c.RetryAfter, "QUEUE_RETRY_AFTER", "REDIS_QUEUE_RETRY_AFTER")
	_ = envDuration(&c.BlockFor, "QUEUE_BLOCK_FOR")

	_ = envInt(&c.Concurrency, "QUEUE_CONCURRENCY")
	_ = envInt(&c.MaxJobs, "QUEUE_MAX_JOBS")
	_ = envInt(&c.MaxTime, "QUEUE_MAX_TIME")
	_ = envDuration(&c.Sleep, "QUEUE_SLEEP")
	_ = envInt(&c.MigrationBatchSize, "QUEUE_MIGRATION_BATCH_SIZE")

	envString(&c.FailedJobsTable, "QUEUE_FAILED_TABLE")
	envString(&c.FailedJobsDriver, "QUEUE_FAILED_DRIVER")
	if v := os.Getenv("DATABASE_URL"); v != "" {
		c.DatabaseDSN = v
	}

	return c
}

// Load is FromEnv plus validation: the returned Config is the same, and the
// error (an errors.Join) names every variable whose value could not be
// parsed and was therefore left at its default.
func Load() (*Config, error) {
	var scratch Config
	return FromEnv(), errors.Join(
		envInt(&scratch.RedisDB, "REDIS_DB"),
		envDuration(&scratch.RetryAfter, "QUEUE_RETRY_AFTER", "REDIS_QUEUE_RETRY_AFTER"),
		envDuration(&scratch.BlockFor, "QUEUE_BLOCK_FOR"),
		envInt(&scratch.Concurrency, "QUEUE_CONCURRENCY"),
		envInt(&scratch.MaxJobs, "QUEUE_MAX_JOBS"),
		envInt(&scratch.MaxTime, "QUEUE_MAX_TIME"),
		envDuration(&scratch.Sleep, "QUEUE_SLEEP"),
		envInt(&scratch.MigrationBatchSize, "QUEUE_MIGRATION_BATCH_SIZE"),
	)
}

// envString sets dst from the first of names that is set.
func envString(dst *string, names ...string) {
	for _, name := range names {
		if v := envValue(name); v != "" {
			*dst = v
			return
		}
	}
}

// envInt sets dst from the first of names that is set. An unparsable value
// leaves dst alone and is returned as an error.
func envInt(dst *int, names ...string) error {
	for _, name := range names {
		v := envValue(name)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s=%q is not an integer", name, v)
		}
		*dst = n
		return nil
	}
	return nil
}

// envDuration is envInt for durations; see ParseDuration.
func envDuration(dst *time.Duration, names ...string) error {
	for _, name := range names {
		v := envValue(name)
		if v == "" {
			continue
		}
		d, err := ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%s=%q is not a duration", name, v)
		}
		*dst = d
		return nil
	}
	return nil
}

// envValue reads a variable the way Laravel's env() helper does: surrounding
// quotes are dropped and "null" / "(null)" / "empty" / "(empty)" mean "not set".
func envValue(name string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	}
	switch strings.ToLower(v) {
	case "null", "(null)", "empty", "(empty)":
		return ""
	}
	return v
}

// redisAddr builds "host:port". A host that already carries a port is kept
// as it is, for compatibility with REDIS_HOST=host:port.
func redisAddr(host, port string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if port == "" {
		port = "6379"
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), port)
}

// ParseDuration parses a Go duration ("90s", "1m30s") or a bare, possibly
// fractional, number of seconds ("90", "0.5"), which is how Laravel's
// configuration and queue:work flags express durations.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if seconds, err := strconv.ParseFloat(s, 64); err == nil {
		if seconds < 0 {
			return 0, fmt.Errorf("negative duration %q", s)
		}
		return time.Duration(seconds * float64(time.Second)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("negative duration %q", s)
	}
	return d, nil
}
