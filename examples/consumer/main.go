package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rjp2525/laravel-queue-go/pkg/events"
	"github.com/rjp2525/laravel-queue-go/pkg/middleware"
	"github.com/rjp2525/laravel-queue-go/pkg/queue"
)

func main() {
	rdb := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
	})

	driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
		Prefix:     "laravel-database-",
		RetryAfter: 90 * time.Second,
		BlockFor:   5 * time.Second,
	})

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	mgr := queue.NewManager(driver)
	mgr.SetLogger(logger)

	mgr.Register(`App\Jobs\SendWelcomeEmail`, func(ctx context.Context, job *queue.Job) error {
		email := job.GetString("email")
		userID := job.GetInt("user_id")
		fmt.Printf("Sending welcome email to %s (user #%d)\n", email, userID)
		return nil
	})

	mgr.Register(`App\Jobs\ProcessOrder`, func(ctx context.Context, job *queue.Job) error {
		orderID := job.GetInt("order_id")
		amount := job.GetFloat("amount")
		fmt.Printf("Processing order #%d for $%.2f\n", orderID, amount)

		if model := job.GetModelID("user"); model != nil {
			fmt.Printf("  User model: %s #%v\n", model.Class, model.ID)
		}

		return nil
	})

	mgr.RegisterDefault(func(ctx context.Context, job *queue.Job) error {
		fmt.Printf("Unhandled job: %s (uuid=%s)\n", job.DisplayName(), job.UUID())
		return nil
	})

	// Timeout middleware now receives middleware.Job directly — no type assertion needed.
	mgr.Use(middleware.Timeout(func(job middleware.Job) *time.Duration {
		if t := job.Timeout(); t != nil {
			d := time.Duration(*t) * time.Second
			return &d
		}
		return nil
	}))

	mgr.On(events.JobProcessed, func(e events.Event) {
		logger.Info("job completed", "job", e.JobName, "duration", e.Duration)
	})

	mgr.On(events.JobFailed, func(e events.Event) {
		logger.Error("job failed permanently", "job", e.JobName, "error", e.Error)
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	worker := mgr.Worker(queue.WorkerOptions{
		Queue:       "default",
		Concurrency: 4,
		Sleep:       3 * time.Second,
	})

	logger.Info("starting consumer", "queue", "default", "concurrency", 4)

	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("worker error: %v", err)
	}

	logger.Info("consumer stopped gracefully")
}
