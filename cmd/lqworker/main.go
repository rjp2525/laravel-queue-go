package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rjp2525/laravel-queue-go/pkg/config"
	"github.com/rjp2525/laravel-queue-go/pkg/queue"
)

var version = "dev"

func main() {
	// Flags matching Laravel's queue:work command.
	var (
		queueName   string
		connection  string
		sleep       time.Duration
		retryAfter  time.Duration
		blockFor    time.Duration
		maxJobs     int
		maxTime     int
		concurrency int
		redisAddr   string
		redisPass   string
		redisDB     int
		prefix      string
		showVersion bool
	)

	flag.StringVar(&queueName, "queue", "", "Queue name(s) to process (comma-separated)")
	flag.StringVar(&connection, "connection", "redis", "Connection name")
	flag.DurationVar(&sleep, "sleep", 0, "Seconds to sleep when no jobs available")
	flag.DurationVar(&retryAfter, "retry-after", 0, "Seconds before retrying a job")
	flag.DurationVar(&blockFor, "block-for", 0, "Seconds to block waiting for jobs")
	flag.IntVar(&maxJobs, "max-jobs", 0, "Max jobs to process before stopping (0=unlimited)")
	flag.IntVar(&maxTime, "max-time", 0, "Max seconds to run before stopping (0=unlimited)")
	flag.IntVar(&concurrency, "concurrency", 0, "Number of concurrent workers")
	flag.StringVar(&redisAddr, "redis", "", "Redis address (host:port)")
	flag.StringVar(&redisPass, "redis-password", "", "Redis password")
	flag.IntVar(&redisDB, "redis-db", -1, "Redis database number")
	flag.StringVar(&prefix, "prefix", "", "Redis key prefix")
	flag.BoolVar(&showVersion, "version", false, "Show version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "lqworker - Laravel Queue Worker for Go\n\n")
		fmt.Fprintf(os.Stderr, "Usage: lqworker [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Processes jobs from a Laravel Redis queue.\n\n")
		fmt.Fprintf(os.Stderr, "Environment variables can be used for all options (see config package).\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("lqworker %s\n", version)
		return
	}

	// Load config from environment, then override with flags.
	cfg := config.FromEnv()

	if queueName != "" {
		cfg.DefaultQueue = queueName
	}
	if sleep > 0 {
		cfg.Sleep = sleep
	}
	if retryAfter > 0 {
		cfg.RetryAfter = retryAfter
	}
	if blockFor > 0 {
		cfg.BlockFor = blockFor
	}
	if maxJobs > 0 {
		cfg.MaxJobs = maxJobs
	}
	if maxTime > 0 {
		cfg.MaxTime = maxTime
	}
	if concurrency > 0 {
		cfg.Concurrency = concurrency
	}
	if redisAddr != "" {
		cfg.RedisAddr = redisAddr
	}
	if redisPass != "" {
		cfg.RedisPassword = redisPass
	}
	if redisDB >= 0 {
		cfg.RedisDB = redisDB
	}
	if prefix != "" {
		cfg.RedisPrefix = prefix
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Connect to Redis.
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Verify Redis connection.
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Error("failed to connect to Redis", "addr", cfg.RedisAddr, "error", err)
		os.Exit(1)
	}

	// Create the Redis driver.
	driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
		Prefix:     cfg.RedisPrefix,
		RetryAfter: cfg.RetryAfter,
		BlockFor:   cfg.BlockFor,
		BatchSize:  cfg.MigrationBatchSize,
	})

	// Create the manager.
	mgr := queue.NewManager(driver)
	mgr.SetLogger(logger)
	mgr.SetConnection(connection)

	// Register a default handler that logs unhandled jobs.
	// In a real application, users would import this as a library and register handlers.
	mgr.RegisterDefault(func(ctx context.Context, job *queue.Job) error {
		logger.Info("received job (no handler registered — use as library to register handlers)",
			"job", job.DisplayName(),
			"uuid", job.UUID(),
			"attempts", job.Attempts(),
		)
		return nil
	})

	// Process comma-separated queue names.
	queues := strings.Split(cfg.DefaultQueue, ",")
	queueToProcess := queues[0]
	if len(queues) > 1 {
		logger.Info("multiple queues specified — processing first queue",
			"queues", queues,
			"processing", queueToProcess,
		)
	}

	// Create and start the worker.
	worker := mgr.Worker(queue.WorkerOptions{
		Queue:       queueToProcess,
		Concurrency: cfg.Concurrency,
		MaxJobs:     cfg.MaxJobs,
		MaxTime:     cfg.MaxTime,
		Sleep:       cfg.Sleep,
	})

	logger.Info("starting worker",
		"queue", queueToProcess,
		"concurrency", cfg.Concurrency,
		"redis", cfg.RedisAddr,
		"prefix", cfg.RedisPrefix,
	)

	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("worker stopped with error", "error", err)
		os.Exit(1)
	}

	logger.Info("worker stopped gracefully")
}
