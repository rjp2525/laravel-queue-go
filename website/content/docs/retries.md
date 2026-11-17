---
title: Retries and timeouts
description: What happens when a handler returns an error and how $tries, $backoff, retryUntil and $timeout carry over.
---

The short version: the properties you already put on Laravel jobs mean the same thing here.

## The rules

When a handler returns an error (or panics), the worker decides between **retrying** and **failing** the job:

| On the job | Effect |
|---|---|
| `$tries = 3` | Run at most 3 times. The third failure is final. |
| `$tries = 0` | No limit. |
| no `$tries` | Use the worker's `MaxTries`. Its default, `0`, means no limit. |
| `retryUntil()` | Keep retrying until that time. While it's set, `$tries` is ignored. |
| `$backoff = 30` | Wait 30 seconds before each retry. |
| `$backoff = [10, 60, 300]` | Wait 10s, then 60s, then 300s for every retry after that. |
| no `$backoff` | Use the worker's `Backoff`. Its default is no delay. |

A job that fails for good is removed from the queue, written to [failed job storage](/docs/failed-jobs/) and announced with a `JobFailed` event.

`job.Attempts()` counts the current run, so it's `1` the first time, exactly like `$this->attempts()`.

```go
mgr.Register(`App\Jobs\SyncInventory`, func(ctx context.Context, job *queue.Job) error {
    if err := sync(ctx); err != nil {
        log.Printf("attempt %d failed: %v", job.Attempts(), err)
        return err // retried per $tries and $backoff
    }
    return nil
})
```

## Worker-level defaults

For jobs that don't declare their own:

```go
worker := mgr.Worker(queue.WorkerOptions{
    Queue:    "default",
    MaxTries: 3,                // like queue:work --tries=3
    Backoff:  10 * time.Second, // like queue:work --backoff=10
    Timeout:  60 * time.Second, // like queue:work --timeout=60
})
```

A value on the job always wins over the worker's.

## Timeouts

If a job has a `$timeout` (or the worker has a `Timeout`), the `ctx` passed to the handler is cancelled when it runs out.

```go
mgr.Register(`App\Jobs\FetchReport`, func(ctx context.Context, job *queue.Job) error {
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, reportURL, nil)
    resp, err := http.DefaultClient.Do(req) // aborted when the timeout hits
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    // ...
})
```

When a handler returns an error after its deadline, the failure is reported as a `*queue.TimeoutError`. With `$failOnTimeout = true` the job is failed on the spot instead of retried.

> [!WARNING]
> Timeouts are cooperative. Go can't kill a goroutine, so a handler that ignores `ctx` runs for as long as it likes. Pass `ctx` to database calls, HTTP requests and anything else that can block and check `ctx.Err()` inside long loops.

Keep `$timeout` shorter than the queue's `retry_after`. Otherwise Redis gives the job to another worker while the first one is still running it.

## When a worker dies mid-job

Attempts are counted in Redis the moment a job is reserved, not when it finishes. So if the process is killed (OOM, `kill -9`, a node going away), the attempt still counts. After `retry_after` the job becomes available again. Once it has used up its attempts it is failed with `queue.ErrMaxAttemptsExceeded` rather than run again.

This is what stops a job that reliably crashes its worker from doing so forever, which is the reason to set `$tries` or `MaxTries` at all.

## Telling errors apart

```go
mgr.On(events.JobFailed, func(e events.Event) {
    var timeout *queue.TimeoutError
    var panicked *queue.PanicError

    switch {
    case errors.As(e.Error, &timeout):
        metrics.Inc("jobs.timed_out")
    case errors.As(e.Error, &panicked):
        log.Printf("panic in %s:\n%s", e.JobName, panicked.Stack)
    case errors.Is(e.Error, queue.ErrMaxAttemptsExceeded):
        // the worker died on every attempt; the handler never returned
    case errors.Is(e.Error, queue.ErrNoHandler):
        // nobody registered a handler for this class
    }
})
```

## Not retrying

Some failures won't get better: a record that no longer exists, a payload that can't be valid. Returning an error would burn through the retries for nothing. Decide in the handler:

```go
order, err := orders.Find(ctx, id)
if errors.Is(err, sql.ErrNoRows) {
    log.Printf("order %d is gone, skipping", id)
    return nil // done, nothing to retry
}
```

## What isn't enforced

`$maxExceptions` is read from the payload but not applied. Laravel keeps that counter in the application cache, which a Go worker has no access to. Use `$tries` instead.
