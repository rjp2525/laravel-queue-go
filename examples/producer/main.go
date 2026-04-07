// Package main demonstrates dispatching jobs for Laravel workers to consume.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rjp2525/laravel-queue-go/pkg/queue"
)

func main() {
	// 1. Create a Redis client.
	rdb := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "",
		DB:       0,
	})

	ctx := context.Background()

	// Verify connection.
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to Redis: %v", err)
	}

	// 2. Create the Redis queue driver.
	driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
		Prefix: "laravel-database-",
	})

	// 3. Create a dispatcher with a default queue.
	d := queue.NewDispatcher(driver, queue.WithDefaultQueue("default"))

	// 4. Simple dispatch -- Laravel's `dispatch(new SendWelcomeEmail(...))`.
	err := d.Dispatch(ctx, `App\Jobs\SendWelcomeEmail`, map[string]any{
		"email":   "user@example.com",
		"user_id": 42,
	}, queue.WithMaxTries(3))
	if err != nil {
		log.Fatalf("dispatch SendWelcomeEmail: %v", err)
	}
	fmt.Println("Dispatched SendWelcomeEmail")

	// 5. Delayed dispatch -- Laravel's `dispatch(...)->delay(30)`.
	err = d.Later(ctx, 30*time.Second, `App\Jobs\SendReminder`, map[string]any{
		"user_id": 42,
		"message": "Don't forget to complete your profile!",
	})
	if err != nil {
		log.Fatalf("dispatch SendReminder: %v", err)
	}
	fmt.Println("Dispatched SendReminder (30s delay)")

	// 6. Full control with DispatchOptions -- chained jobs, backoff, tags.
	maxTries := 5
	maxExceptions := 3
	timeout := 120

	err = d.DispatchWithOptions(ctx, queue.DispatchOptions{
		Job:           `App\Jobs\ProcessOrder`,
		Queue:         "high",
		Delay:         0,
		MaxTries:      &maxTries,
		MaxExceptions: &maxExceptions,
		FailOnTimeout: true,
		Backoff:       []int{10, 30, 60},
		Timeout:       &timeout,
		Tags:          []string{"order", "payment"},
		Args: map[string]any{
			"order_id": 1001,
			"amount":   99.99,
			"currency": "USD",
		},
		Chain: []queue.ChainedJob{
			{
				Job:  `App\Jobs\SendReceipt`,
				Args: map[string]any{"order_id": 1001},
			},
			{
				Job:  `App\Jobs\NotifyWarehouse`,
				Args: map[string]any{"order_id": 1001},
			},
		},
	})
	if err != nil {
		log.Fatalf("dispatch ProcessOrder: %v", err)
	}
	fmt.Println("Dispatched ProcessOrder with chain (queue=high)")

	// 7. Dispatch with tags and retry deadline.
	retryUntil := time.Now().Add(1 * time.Hour).Unix()
	err = d.Dispatch(ctx, `App\Jobs\SyncInventory`, map[string]any{
		"warehouse_id": 5,
	},
		queue.WithRetryUntil(retryUntil),
		queue.WithTags("inventory", "sync"),
		queue.WithBackoff(60),
	)
	if err != nil {
		log.Fatalf("dispatch SyncInventory: %v", err)
	}
	fmt.Println("Dispatched SyncInventory")

	fmt.Println("All jobs dispatched successfully.")
}
