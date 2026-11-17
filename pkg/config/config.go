package config

import (
	"os"
	"strconv"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/queue"
)

type Config struct {
	// Redis
	RedisAddr     string
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

	// LaravelVersion is the major version of the Laravel app sharing the
	// queue (LARAVEL_VERSION). queue.LaravelAuto detects it from queued jobs.
	LaravelVersion queue.LaravelVersion
	// AppName is the Laravel APP_NAME, used to derive RedisPrefix when
	// REDIS_PREFIX is not set.
	AppName string
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

func FromEnv() *Config {
	c := DefaultConfig()

	if v := os.Getenv("REDIS_HOST"); v != "" {
		c.RedisAddr = v
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		c.RedisPassword = v
	}
	if v := os.Getenv("REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RedisDB = n
		}
	}
	if v := os.Getenv("REDIS_PREFIX"); v != "" {
		c.RedisPrefix = v
	}
	if v := os.Getenv("QUEUE_NAME"); v != "" {
		c.DefaultQueue = v
	}
	if v := os.Getenv("QUEUE_RETRY_AFTER"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.RetryAfter = d
		}
	}
	if v := os.Getenv("QUEUE_BLOCK_FOR"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.BlockFor = d
		}
	}
	if v := os.Getenv("QUEUE_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Concurrency = n
		}
	}
	if v := os.Getenv("QUEUE_MAX_JOBS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MaxJobs = n
		}
	}
	if v := os.Getenv("QUEUE_MAX_TIME"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MaxTime = n
		}
	}
	if v := os.Getenv("QUEUE_SLEEP"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.Sleep = d
		}
	}
	if v := os.Getenv("QUEUE_MIGRATION_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MigrationBatchSize = n
		}
	}
	if v := os.Getenv("QUEUE_FAILED_TABLE"); v != "" {
		c.FailedJobsTable = v
	}
	if v := os.Getenv("QUEUE_FAILED_DRIVER"); v != "" {
		c.FailedJobsDriver = v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		c.DatabaseDSN = v
	}
	if v := os.Getenv("LARAVEL_VERSION"); v != "" {
		if lv, err := queue.ParseLaravelVersion(v); err == nil {
			c.LaravelVersion = lv
		}
	}
	c.AppName = os.Getenv("APP_NAME")

	// Without an explicit REDIS_PREFIX, use the prefix Laravel itself derives
	// from APP_NAME: "{slug}_database_" on Laravel 10/11, "{slug}-database-"
	// on 12/13.
	if os.Getenv("REDIS_PREFIX") == "" {
		c.RedisPrefix = c.LaravelVersion.RedisPrefix(c.AppName)
	}

	return c
}
