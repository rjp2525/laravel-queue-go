---
title: Going to production
description: A short checklist, then the reasoning behind each item.
---

## The checklist

- [ ] `Prefix` is set explicitly and matches Laravel.
- [ ] `LaravelVersion` is set explicitly.
- [ ] `RetryAfter` is longer than your slowest job, and jobs have a `$timeout` shorter than `RetryAfter`.
- [ ] Jobs have `$tries`, or the worker has `MaxTries`.
- [ ] A failed job provider is configured.
- [ ] Go workers consume queues that carry only jobs they have handlers for.
- [ ] Handlers pass `ctx` to everything that blocks.
- [ ] Handlers are idempotent.
- [ ] Shutdown drains before it cancels.
- [ ] Something is watching queue depth and failures.

## Why each one matters

### Explicit prefix and version

Both have sensible defaults and both defaults are guesses. A wrong prefix doesn't produce an error. The worker connects, finds an empty queue and waits forever. Pin them and that class of problem is gone. See [installation](/docs/installation/#1-the-redis-key-prefix).

### `RetryAfter`, `$timeout`, and why they're a pair

A reserved job that isn't finished within `RetryAfter` is assumed lost and given to another worker. If the first worker was merely slow, you now have the job running twice.

So: `$timeout` < `RetryAfter`. The timeout stops the handler, and only later does Redis consider the job abandoned.

### A retry limit

With no `$tries` on the job and `MaxTries: 0` on the worker, a job that always fails is retried forever. Pick a number. Laravel's default is 1.

### Failed job storage

The default provider throws failures away. Wire up [`failed.NewDatabaseProvider`](/docs/failed-jobs/) so `queue:failed` and `queue:retry` work, or you'll find out about failures from your users.

### Dedicated queues

A Go worker fails any job it has no handler for. That's deliberate, since quietly deleting it would be worse, but it means a Go worker on `default` will fail every PHP job it reaches before a PHP worker does. Route Go's jobs with `->onQueue('go')` or a `$queue` property on the class.

### Idempotent handlers

Redis queues are at-least-once. A job can run twice: a worker is killed after doing the work but before acknowledging it, or a job outlives `RetryAfter`. Write handlers so a second run is harmless: upserts instead of inserts, a "done" marker checked first, idempotency keys on outbound API calls.

### Graceful shutdown

On deploy, call `worker.Stop()` first and cancel the context only after a grace period. The [workers page](/docs/workers/#shutting-down) has the code. Make sure your process manager waits at least that long before it sends `SIGKILL`.

## Watch the queue

The driver reports depth per state:

```go
pending, _ := driver.PendingSize(ctx, "go")   // waiting to run
delayed, _ := driver.DelayedSize(ctx, "go")   // scheduled, or backing off before a retry
reserved, _ := driver.ReservedSize(ctx, "go") // being worked on right now
total, _ := driver.Size(ctx, "go")            // all three, what Laravel's Queue::size() reports
```

What to alert on:

- **`pending` climbing** means workers can't keep up. Add concurrency or workers.
- **`delayed` climbing** usually means jobs are failing and backing off.
- **`reserved` stuck above your concurrency** means jobs are being abandoned, likely workers dying mid-job.
- **Any `JobFailed` [event](/docs/events/).**

## Sizing

One Go process with `Concurrency: N` holds at most `N` jobs. Start with:

- **I/O-bound jobs:** `Concurrency` of 20 to 100 per process, then raise it until the service you're calling pushes back.
- **CPU-bound jobs:** `Concurrency` equal to the cores available to the container.

Give the Redis client a pool at least as large as `Concurrency` plus a few (`redis.Options.PoolSize`), since each running job may be talking to Redis through your own code as well.

Scale out by running more processes on the same queue. They coordinate through Redis; there is nothing to configure between them.

## Running Go and PHP workers on the same queue

It works, and it's occasionally useful for a gradual migration, but both sides need a handler for every job class on that queue. In PHP that means a `handle()` method; in Go, a registered handler. Whichever worker reserves the job runs it. For anything long-lived, separate queues are easier to reason about.

## Horizon

Go workers don't report to Horizon. Horizon shows queue lengths for queues it knows about, but not Go workers' throughput, runtimes or processes. Use [events](/docs/events/) to export your own metrics.
