# laravel-queue-go

[![Go Reference](https://pkg.go.dev/badge/github.com/rjp2525/laravel-queue-go.svg)](https://pkg.go.dev/github.com/rjp2525/laravel-queue-go)
[![CI](https://github.com/rjp2525/laravel-queue-go/actions/workflows/ci.yml/badge.svg)](https://github.com/rjp2525/laravel-queue-go/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/rjp2525/laravel-queue-go)](https://goreportcard.com/report/github.com/rjp2525/laravel-queue-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Go package for interop with Laravel's Redis queue system. Consume jobs dispatched by Laravel and dispatch jobs for Laravel workers to pick up. Supports both PHP-serialized and JSON-encoded command payloads.

## Requirements

- Go 1.26+
- Redis 6+

## Installation

```bash
go get github.com/rjp2525/laravel-queue-go
```

## Quick start

### Consuming Laravel jobs

Register a handler for each Laravel job class (FQCN), create a worker, and run it:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os/signal"
    "syscall"

    "github.com/redis/go-redis/v9"
    "github.com/rjp2525/laravel-queue-go/pkg/queue"
)

func main() {
    rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
    defer rdb.Close()

    driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
        Prefix: "laravel-database-",
    })

    mgr := queue.NewManager(driver)

    mgr.Register(`App\Jobs\SendWelcomeEmail`, func(ctx context.Context, job *queue.Job) error {
        email := job.GetString("email")
        fmt.Println("sending welcome email to", email)
        return nil
    })

    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
    defer cancel()

    worker := mgr.Worker(queue.WorkerOptions{
        Queue:       "default",
        Concurrency: 4,
    })

    if err := worker.Run(ctx); err != nil {
        log.Fatal(err)
    }
}
```

### Dispatching jobs for Laravel

Create a dispatcher and push jobs that Laravel workers will pick up:

```go
d := queue.NewDispatcher(driver, queue.WithDefaultQueue("default"))

// Immediate dispatch.
d.Dispatch(ctx, `App\Jobs\ProcessPayment`, map[string]any{
    "order_id": 42,
    "amount":   99.99,
}, queue.WithMaxTries(3))

// Delayed dispatch.
d.Later(ctx, 30*time.Second, `App\Jobs\SendReminder`, map[string]any{
    "user_id": 7,
})
```

## API overview

### Manager

`Manager` ties together handlers, middleware, events, and worker/dispatcher creation.

```go
mgr := queue.NewManager(driver)
mgr.Register(`App\Jobs\MyJob`, myHandler)
mgr.RegisterDefault(fallbackHandler)      // catch-all for unregistered job types
mgr.Use(myMiddleware)                      // global middleware
mgr.On(events.JobFailed, myListener)       // event listeners
mgr.SetFailedProvider(dbProvider)          // failed job storage (accepts failed.Logger or failed.Store)
mgr.SetLogger(logger)
mgr.SetConnection("redis")

worker := mgr.Worker(queue.WorkerOptions{...})
dispatcher := mgr.Dispatcher()
```

### Worker configuration

```go
queue.WorkerOptions{
    Queue:       "default",      // queue name to process
    Concurrency: 4,              // parallel goroutines (1 = serial)
    MaxJobs:     1000,           // stop after N jobs (0 = unlimited)
    MaxTime:     3600,           // stop after N seconds (0 = unlimited)
    Sleep:       3*time.Second,  // poll interval when queue is empty
    StopOnEmpty: false,          // exit when queue drains
}
```

### Handler registration

Handlers are registered by the Laravel FQCN that appears in the job payload's `commandName`:

```go
mgr.Register(`App\Jobs\SendWelcomeEmail`, func(ctx context.Context, job *queue.Job) error {
    return nil
})
```

The worker resolves handlers by trying `job.CommandName()` first, then `job.DisplayName()`, and finally the default handler.

### Job property access

The `Job` type provides typed accessors for deserialized command properties. These work the same whether the command was PHP-serialized or JSON-encoded:

```go
job.GetString("email")         // string
job.GetInt("user_id")          // int64
job.GetFloat("amount")         // float64
job.GetBool("is_active")       // bool
job.GetSlice("tags")           // []any
job.GetMap("metadata")         // map[string]any
job.GetEnum("status")          // *phpserialize.Enum (PHP 8.1 backed enum)
job.GetModelID("user")         // *phpserialize.ModelIdentifier (Eloquent model)
```

Metadata:

```go
job.UUID()           // job UUID
job.DisplayName()    // Laravel display name
job.CommandName()    // FQCN
job.Attempts()       // attempt count
job.Queue()          // queue name
job.MaxTries()       // *int
job.Timeout()        // *int (seconds)
job.Tags()           // []string
job.BatchID()        // batch ID if part of a batch
job.ChainedJobs()    // chained job payloads
job.Format()         // queue.FormatPHP or queue.FormatJSON
```

Lifecycle:

```go
job.Delete()               // ACK: remove from reserved set
job.Release(5*time.Second) // NACK: return to queue with delay
job.HasFailed()            // true if retry limits exceeded
```

### Command format (PHP vs JSON)

By default, the dispatcher PHP-serializes the `data.command` field for Laravel compatibility. For Go-to-Go workers or custom setups, you can use JSON instead:

```go
d := queue.NewDispatcher(driver,
    queue.WithDefaultQueue("default"),
    queue.WithCommandFormat(queue.FormatJSON),
)
```

When consuming, the worker auto-detects the format. No configuration needed.

### Dispatcher

```go
d := queue.NewDispatcher(driver, queue.WithDefaultQueue("emails"))

d.Dispatch(ctx, `App\Jobs\MyJob`, map[string]any{"key": "value"})

d.Later(ctx, 30*time.Second, `App\Jobs\MyJob`, args)

d.DispatchWithOptions(ctx, queue.DispatchOptions{
    Job:           `App\Jobs\MyJob`,
    Queue:         "high",
    Delay:         10 * time.Second,
    MaxTries:      intPtr(5),
    MaxExceptions: intPtr(3),
    FailOnTimeout: true,
    Backoff:       []int{10, 30, 60},
    Timeout:       intPtr(120),
    Tags:          []string{"order", "payment"},
    Args:          map[string]any{"order_id": 42},
    Chain: []queue.ChainedJob{
        {Job: `App\Jobs\SendReceipt`, Args: map[string]any{"order_id": 42}},
        {Job: `App\Jobs\NotifyAdmin`, Args: map[string]any{"order_id": 42}},
    },
})
```

### Payload options

Append these to `Dispatch` or `Later` calls:

```go
queue.WithMaxTries(3)
queue.WithMaxExceptions(2)
queue.WithFailOnTimeout(true)
queue.WithBackoff([]int{10, 30, 60})  // progressive backoff in seconds
queue.WithTimeout(120)                 // job timeout in seconds
queue.WithRetryUntil(timestamp)        // unix timestamp deadline
queue.WithTags("order", "payment")
```

### Middleware

The middleware pipeline wraps job execution. Each middleware calls `next()` to proceed:

```go
import "github.com/rjp2525/laravel-queue-go/pkg/middleware"
```

`Timeout` enforces a per-job timeout:

```go
middleware.Timeout(func(job middleware.Job) *time.Duration {
    if t := job.Timeout(); t != nil {
        d := time.Duration(*t) * time.Second
        return &d
    }
    return nil
})
```

`RateLimited` throttles processing to N jobs per decay window:

```go
middleware.RateLimited(redisClient, "emails", 10, time.Minute)
```

`WithoutOverlapping` prevents concurrent execution of jobs with the same key:

```go
middleware.WithoutOverlapping(redisClient, func(job middleware.Job) string {
    return job.CommandName()
}, 60*time.Second)
```

`Unique` ensures only one instance of a job runs within a TTL:

```go
middleware.Unique(redisClient, func(job middleware.Job) string {
    return job.CommandName()
}, 5*time.Minute)
```

`ThrottlesExceptions` backs off after repeated failures:

```go
middleware.ThrottlesExceptions(redisClient, func(job middleware.Job) string {
    return job.CommandName()
}, 5, 10) // 5 exceptions per 10 minutes
```

Apply middleware globally:

```go
mgr.Use(middleware.Timeout(timeoutGetter), middleware.RateLimited(rdb, "api", 60, time.Minute))
```

### Events

```go
import "github.com/rjp2525/laravel-queue-go/pkg/events"

mgr.On(events.JobProcessing, func(e events.Event) {
    log.Println("processing", e.JobName, "attempt", e.Attempt)
})

mgr.On(events.JobProcessed, func(e events.Event) {
    log.Println("completed", e.JobName, "in", e.Duration)
})

mgr.On(events.JobFailed, func(e events.Event) {
    log.Println("failed", e.JobName, e.Error)
})

mgr.On(events.JobExceptionOccurred, func(e events.Event) {
    log.Println("exception in", e.JobName, e.Error)
})

mgr.On(events.WorkerStopping, func(e events.Event) {
    log.Println("worker shutting down")
})
```

### Failed job providers

Failed jobs can be recorded to a database table matching Laravel's `failed_jobs` migration:

```go
import (
    "database/sql"
    "github.com/rjp2525/laravel-queue-go/pkg/failed"
)

db, _ := sql.Open("mysql", dsn)
provider, err := failed.NewDatabaseProvider(db, "failed_jobs")
if err != nil {
    log.Fatal(err)
}
mgr.SetFailedProvider(provider)
```

The worker only needs the `failed.Logger` interface (just `Log`). `DatabaseProvider` and `NullProvider` both implement the full `failed.Store` interface, which adds `Find`, `All`, `Forget`, `Flush`, and `Count`. A `NullProvider` is used by default and discards failures.

### Batch support

Jobs dispatched as part of a Laravel batch include a `batchId` property:

```go
if batchID := job.BatchID(); batchID != "" {
    log.Println("processing batch", batchID)
}
```

Chained jobs:

```go
chain := job.ChainedJobs() // []string of serialized payloads
```

### PHP serialization

The `phpserialize` package handles PHP `serialize()` / `unserialize()` for Laravel job command data:

```go
import "github.com/rjp2525/laravel-queue-go/pkg/phpserialize"

// Decode a PHP-serialized string.
val, err := phpserialize.Decode(serializedString)
obj := val.(*phpserialize.Object)

// Encode a Go map as a PHP serialized object.
encoded, err := phpserialize.MarshalObject("App\\Jobs\\MyJob", map[string]any{
    "userId": 42,
})

// Access properties.
name := phpserialize.GetString(obj, "name")
```

The decoder handles objects, arrays, strings, integers, floats, booleans, null, PHP 8.1 backed enums (`E:` format), and PHP property visibility (public/protected/private null-byte prefixes). `ModelIdentifier` support is built in for Eloquent model references.

Enum values are decoded into `*phpserialize.Enum` with `ClassName` and `CaseName` fields. `GetString` also works on enum properties and returns the case name.

### Configuration

All settings can be loaded from environment variables:

| Variable | Default | Description |
|---|---|---|
| `REDIS_HOST` | `127.0.0.1:6379` | Redis address |
| `REDIS_PASSWORD` | (empty) | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `REDIS_PREFIX` | `laravel-database-` | Redis key prefix |
| `QUEUE_NAME` | `default` | Default queue name |
| `QUEUE_RETRY_AFTER` | `90s` | Reserved job visibility timeout |
| `QUEUE_BLOCK_FOR` | `5s` | BLPOP notification timeout |
| `QUEUE_CONCURRENCY` | `1` | Worker goroutine count |
| `QUEUE_MAX_JOBS` | `0` | Stop after N jobs (0 = unlimited) |
| `QUEUE_MAX_TIME` | `0` | Stop after N seconds (0 = unlimited) |
| `QUEUE_SLEEP` | `3s` | Empty-queue poll interval |
| `QUEUE_MIGRATION_BATCH_SIZE` | `100` | Expired job migration batch size |
| `QUEUE_FAILED_TABLE` | `failed_jobs` | Failed jobs table name |
| `QUEUE_FAILED_DRIVER` | `database` | Failed job storage driver |
| `DATABASE_URL` | (empty) | Database DSN for failed jobs |

```go
import "github.com/rjp2525/laravel-queue-go/pkg/config"

cfg := config.FromEnv()
```

The default Redis prefix (`laravel-database-`) matches what Laravel generates from `Str::slug(env('APP_NAME', 'laravel')).'-database-'`. If your Laravel app has a custom `APP_NAME`, set `REDIS_PREFIX` to match.

### CLI binary (`lqworker`)

A standalone worker binary for quick use without writing Go code:

```bash
go install github.com/rjp2525/laravel-queue-go/cmd/lqworker@latest

lqworker \
  --queue=default \
  --concurrency=4 \
  --redis=127.0.0.1:6379 \
  --prefix=laravel-database- \
  --max-jobs=1000 \
  --max-time=3600
```

All flags can also be set via environment variables. The binary registers a default handler that logs received jobs. For custom handlers, import the library and build your own binary.

## Laravel compatibility

| Laravel Version | Status |
|---|---|
| 12.x | Supported |
| 13.x | Supported |

The Go driver matches Laravel's Redis key layout (`{prefix}queues:{name}`, `:delayed`, `:reserved`, `:notify`), Lua scripts, and JSON payload envelope format. Go workers and Laravel workers can run side-by-side on the same queues.

## Known limitations

- **ShouldBeEncrypted**: Encrypted job payloads are not supported. Jobs must be dispatched without encryption.
- **Closures**: PHP closure-based jobs cannot be deserialized in Go. Only class-based jobs work.
- **Service container**: Laravel's IoC container is not available in Go. Handle dependency resolution in your handler code.
- **Custom serializable objects** (`C:` format): Treated as opaque; properties are not accessible.
- **Horizon**: Go workers do not report metrics to Laravel Horizon. Use structured logging and your own monitoring.

## Contributing

Open an issue to discuss significant changes before submitting a pull request.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-change`)
3. Write tests for new functionality
4. Run `make test` to verify
5. Submit a pull request

## License

MIT License. See [LICENSE](LICENSE) for details.
