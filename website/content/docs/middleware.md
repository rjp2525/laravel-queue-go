---
title: Middleware
description: Wrap every job with your own logic or use the built-in rate limiting and locking.
---

Middleware wraps a handler. It runs before the job, decides whether to continue and sees the result afterwards. If you've written HTTP middleware in Go, this is the same shape.

```go
type Middleware func(ctx context.Context, job middleware.Job, next func() error) error
```

## Writing one

Call `next()` to run the rest of the chain and, eventually, the handler. Whatever you return is the job's result.

```go
func Timing(logger *slog.Logger) middleware.Middleware {
    return func(ctx context.Context, job middleware.Job, next func() error) error {
        start := time.Now()
        err := next()

        logger.Info("job finished",
            "job", job.DisplayName(),
            "attempt", job.Attempts(),
            "took", time.Since(start),
            "error", err,
        )
        return err
    }
}
```

Register it on the manager for every worker or on a single worker:

```go
mgr.Use(Timing(logger), Tracing())

worker.Use(OnlyThisWorker())
```

Middleware runs in the order you register it: the first one is outermost.

Inside middleware you can read the job's metadata (`UUID`, `DisplayName`, `CommandName`, `Queue`, `Attempts`, `MaxTries`, `Timeout`, `Tags`, `BatchID`). To skip a job without failing it, return `nil` without calling `next()`. To send it back to the queue, return a `ReleaseError`:

```go
if maintenanceMode() {
    return &middleware.ReleaseError{Delay: time.Minute}
}
return next()
```

## Rate limiting

Allow at most `maxAttempts` jobs per window. Jobs over the limit go back on the queue until the window has passed.

```go
import "github.com/rjp2525/laravel-queue-go/v2/pkg/middleware"

// at most 60 jobs per minute across every worker sharing this Redis
mgr.Use(middleware.RateLimited(rdb, "mailgun", 60, time.Minute))
```

The key is whatever you choose. Workers that use the same key share the same budget, which is how you respect a third party's API limit across a fleet.

## One at a time

`WithoutOverlapping` makes sure two jobs with the same key never run at the same moment. A job that finds the lock taken is released and retried shortly after.

```go
perAccount := func(job middleware.Job) string {
    return job.DisplayName() // or derive a key from the job
}

mgr.Use(middleware.WithoutOverlapping(rdb, perAccount, 5*time.Minute))
```

The third argument is how long the lock may be held. Set it comfortably above how long the job takes: it's the safety net that frees the lock if a worker dies while holding it. A lock is only ever released by the worker that took it.

`Unique` works the same way but **drops** the second job instead of retrying it. Use it when running the job twice would be pointless rather than harmful.

```go
mgr.Use(middleware.Unique(rdb, perAccount, 5*time.Minute))
```

## Backing off a failing dependency

`ThrottlesExceptions` watches for failures. When a key fails for the `maxExceptions`-th time within the window, the job is released for the length of the window instead of being retried right away.

```go
// after 5 failures in 10 minutes, wait 10 minutes
mgr.Use(middleware.ThrottlesExceptions(rdb, perAccount, 5, 10))
```

## Good to know

> [!NOTE]
> These keys are this package's own. They are not the cache keys Laravel's `WithoutOverlapping`, `RateLimited` or `ShouldBeUnique` use, so a Go job and a PHP job don't lock each other out through this middleware. If both sides must share a limit, enforce it in one place.

- Timeouts don't need middleware. The worker applies the job's `$timeout` itself and cancels the handler's `ctx`. The older `middleware.Timeout` is deprecated because it could stop waiting for a handler but couldn't stop the handler.
- A panic inside middleware is recovered just like one inside a handler.
