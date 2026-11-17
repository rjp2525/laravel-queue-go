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
		redisUser   string
		drainFor    time.Duration
		redisDB     int
		prefix      string
		laravel     string
		showVersion bool
	)

	flag.StringVar(&queueName, "queue", "", "Queue name(s) to process (comma-separated)")
	flag.StringVar(&connection, "connection", "redis", "Connection name")
	durationFlag(&sleep, "sleep", "Time to sleep when no jobs are available, in seconds (3) or as a duration (3s, 500ms)")
	durationFlag(&retryAfter, "retry-after", "Time after which a reserved job is retried, in seconds or as a duration")
	durationFlag(&blockFor, "block-for", "Time to block waiting for a job, in seconds or as a duration")
	durationFlag(&drainFor, "shutdown-timeout", "Time to let running jobs finish after SIGTERM/SIGINT before aborting them, in seconds or as a duration (0 = wait for them)")
	flag.IntVar(&maxJobs, "max-jobs", 0, "Max jobs to process before stopping (0=unlimited)")
	flag.IntVar(&maxTime, "max-time", 0, "Max seconds to run before stopping (0=unlimited)")
	flag.IntVar(&concurrency, "concurrency", 0, "Number of concurrent workers")
	flag.StringVar(&redisAddr, "redis", "", "Redis address (host:port)")
	flag.StringVar(&redisPass, "redis-password", "", "Redis password")
	flag.StringVar(&redisUser, "redis-username", "", "Redis ACL username")
	flag.IntVar(&redisDB, "redis-db", -1, "Redis database number")
	flag.StringVar(&prefix, "prefix", "", "Redis key prefix")
	flag.StringVar(&laravel, "laravel-version", "", "Laravel major version sharing the queue: 10, 11, 12, 13 or auto (default auto)")
	flag.BoolVar(&showVersion, "version", false, "Show version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "lqworker - Laravel Queue Worker for Go\n\n")
		fmt.Fprintf(os.Stderr, "Usage: lqworker [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Processes jobs from a Laravel Redis queue.\n\n")
		fmt.Fprintf(os.Stderr, "WARNING: this binary has no job handlers. It logs every job it receives and\n")
		fmt.Fprintf(os.Stderr, "acknowledges it, which REMOVES the job from the queue. Do not point it at a\n")
		fmt.Fprintf(os.Stderr, "queue whose jobs you need; build your own worker with the library instead.\n\n")
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
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid environment:\n%v\n", err)
		os.Exit(2)
	}

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
	if redisUser != "" {
		cfg.RedisUsername = redisUser
	}
	if redisDB >= 0 {
		cfg.RedisDB = redisDB
	}
	if laravel != "" {
		v, err := queue.ParseLaravelVersion(laravel)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		cfg.LaravelVersion = v
		// The default prefix depends on the version; keep an explicit one.
		if os.Getenv("REDIS_PREFIX") == "" {
			cfg.RedisPrefix = v.RedisPrefix(cfg.AppName)
		}
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
		Username: cfg.RedisUsername,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()

	// The first SIGTERM/SIGINT stops the worker from taking new jobs and lets
	// the running ones finish; ctx is only cancelled by a second signal or
	// when --shutdown-timeout runs out. See shutdownOnSignal.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)

	// Verify Redis connection.
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Error("failed to connect to Redis", "addr", cfg.RedisAddr, "error", err)
		os.Exit(1)
	}

	// Create the Redis driver.
	driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
		Prefix:         cfg.RedisPrefix,
		LaravelVersion: cfg.LaravelVersion,
		RetryAfter:     cfg.RetryAfter,
		BlockFor:       cfg.BlockFor,
		BatchSize:      cfg.MigrationBatchSize,
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

	go shutdownOnSignal(signals, worker.Stop, cancel, drainFor, logger)

	logger.Warn("no job handlers are registered: every job received is logged and acknowledged, which removes it from the queue",
		"queue", queueToProcess,
	)
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

// durationFlag registers a flag that accepts a bare number of seconds, like
// Laravel's queue:work ("--sleep=3"), or a Go duration ("--sleep=500ms").
func durationFlag(dst *time.Duration, name, usage string) {
	flag.Func(name, usage, func(value string) error {
		d, err := config.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("want seconds (3) or a duration (3s, 500ms)")
		}
		*dst = d
		return nil
	})
}

// shutdownOnSignal implements the two-stage shutdown. On the first signal it
// calls stop, which makes the worker finish the jobs it is running and take
// no new ones. If a second signal arrives, or timeout passes (when it is not
// zero), it calls abort, which cancels the context the running jobs are on.
func shutdownOnSignal(signals <-chan os.Signal, stop, abort func(), timeout time.Duration, logger *slog.Logger) {
	sig := <-signals
	logger.Info("shutting down: finishing running jobs, send the signal again to abort them",
		"signal", sig.String(),
		"timeout", timeout.String(),
	)
	stop()

	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}

	select {
	case sig = <-signals:
		logger.Warn("second signal received, aborting running jobs", "signal", sig.String())
	case <-deadline:
		logger.Warn("shutdown timeout reached, aborting running jobs", "timeout", timeout.String())
	}
	abort()
}
