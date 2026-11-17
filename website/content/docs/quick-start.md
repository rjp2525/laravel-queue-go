---
title: Quick start
description: A Laravel job handled by a Go worker, end to end, in about five minutes.
---

We'll take one Laravel job, `SendWelcomeEmail`, and process it in Go.

## 1. The Laravel job

Nothing special here. One detail matters: the Go side reads the job's **properties**, so give them names you're happy to type again.

```php
namespace App\Jobs;

use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Foundation\Queue\Queueable;

class SendWelcomeEmail implements ShouldQueue
{
    use Queueable; // Laravel 10: Dispatchable, InteractsWithQueue, Queueable, SerializesModels

    public $tries = 3;
    public $backoff = [10, 60];

    public function __construct(
        public int $userId,
        public string $email,
    ) {}
}
```

There's no `handle()` method because PHP will never run this job. Dispatch it to a queue only Go workers consume:

```php
SendWelcomeEmail::dispatch($user->id, $user->email)->onQueue('go');
```

## 2. The Go worker

```go
package main

import (
    "context"
    "log"
    "os/signal"
    "syscall"
    "time"

    "github.com/redis/go-redis/v9"
    "github.com/rjp2525/laravel-queue-go/pkg/queue"
)

func main() {
    rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
    defer rdb.Close()

    driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
        Prefix:     "laravel-database-", // must match your app, see Installation
        RetryAfter: 90 * time.Second,
        BlockFor:   5 * time.Second,
    })

    mgr := queue.NewManager(driver)

    mgr.Register(`App\Jobs\SendWelcomeEmail`, func(ctx context.Context, job *queue.Job) error {
        userID := job.GetInt("userId")
        email := job.GetString("email")

        log.Printf("attempt %d: welcoming user %d at %s", job.Attempts(), userID, email)
        return sendWelcome(ctx, email)
    })

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    worker := mgr.Worker(queue.WorkerOptions{
        Queue:       "go",
        Concurrency: 4,
    })

    if err := worker.Run(ctx); err != nil && ctx.Err() == nil {
        log.Fatal(err)
    }
}
```

Three things to notice:

- Handlers are registered by the job's **fully qualified class name**. Use a raw string (backticks) so you don't have to double the backslashes.
- Returning `nil` acknowledges the job. Returning an error retries it, here after 10 seconds and then 60, and after the third failure it goes to `failed_jobs`.
- `ctx` is cancelled on shutdown and when the job's `$timeout` runs out. Pass it to everything that does I/O.

## 3. Run it

```bash
go run .
```

Dispatch the job from Laravel (`php artisan tinker` is the quickest way) and the worker logs it within a moment.

## 4. Going the other way

A Go program can dispatch jobs for Laravel to run. The arguments become the PHP object's public properties:

```go
d := mgr.Dispatcher()

err := d.Dispatch(ctx, `App\Jobs\GenerateInvoice`, map[string]any{
    "orderId": 1042,
    "notify":  true,
})
```

```php
class GenerateInvoice implements ShouldQueue
{
    use Queueable;

    public int $orderId;
    public bool $notify;

    public function handle(): void { /* runs in PHP as usual */ }
}
```

## Where next

- [Workers](/docs/workers/): concurrency, shutdown, limits.
- [Handlers and job data](/docs/handlers/): reading arrays, enums and models.
- [Retries and timeouts](/docs/retries/): what happens when a handler fails.
- [Going to production](/docs/going-to-production/): the checklist before you deploy.
