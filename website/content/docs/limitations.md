---
title: Limitations
description: What this package doesn't do, why not and what to do about it.
---

Better to read this page now than to discover it in production.

## Not supported

### Other queue drivers

Only Laravel's `redis` queue connection. `database`, `sqs`, `beanstalkd` and `sync` use different mechanics entirely.

### Encrypted jobs

Jobs that implement `ShouldBeEncrypted` are encrypted with your `APP_KEY`. A Go worker can't read them. Remove the interface from jobs Go should handle. Keep secrets out of the payload. Pass an ID and look the secret up.

### Queued closures

`dispatch(function () { ... })` serializes PHP code. Go can't run it. Use a job class.

### Running PHP

A Go handler receives the job's data, not its behaviour. `handle()`, the service container, facades and Eloquent stay in PHP. This is the point of the package rather than a gap in it, but it does mean each job a Go worker takes on needs its logic written in Go.

## Partly supported

### `$maxExceptions`

Read from the payload, not enforced. Laravel counts exceptions in the application cache, which Go has no handle on. Use `$tries`.

### Batches

You can read and update `job_batches` but the worker doesn't do it for you. `then` / `catch` / `finally` callbacks only run in a Laravel worker. See [batches](/docs/batches/).

### Chains

Chains dispatched from Go work. A Go worker does not automatically dispatch the next job in a chain it's handling. See [chains](/docs/chains/).

### `ShouldBeUnique`

Laravel takes a lock when it dispatches a unique job and releases it when the job has been processed *by a Laravel worker*. If a Go worker processes the job, that lock is not released and stays until its `uniqueFor` expires. During that time, dispatching the same job again is silently skipped.

If Go handles a unique job, give it a short `uniqueFor`. Or drop `ShouldBeUnique` and deduplicate in the handler.

### Job middleware defined in PHP

A job's `middleware()` method is PHP code and doesn't run in Go. Recreate what you need with [Go middleware](/docs/middleware/). The Go locks and rate limiters use their own Redis keys, separate from Laravel's.

### Horizon

Jobs run by Go workers aren't tracked by Horizon: no throughput, runtime or recent-jobs entries. Export metrics from [events](/docs/events/).

### Queue pausing and `queue:restart`

A Go worker doesn't watch Laravel's restart signal or the pause flags added in Laravel 12. Restart Go workers through your process manager.

### One queue per worker

A worker consumes a single queue. For several queues, run several workers in the same process. There's no `--queue=high,low` priority ordering.

## By design

### At-least-once delivery

A job can run more than once. That's true of Laravel's Redis queue itself and not something a client can change. [Make handlers idempotent](/docs/going-to-production/#idempotent-handlers).

### Timeouts are cooperative

Laravel kills a worker process that exceeds `$timeout`. Go can't kill a goroutine, so the handler's `ctx` is cancelled and the handler is expected to stop. A handler that ignores `ctx` won't time out.

### Jobs without a handler are failed

A Go worker that reserves a job it has no handler for sends it to failed job storage. It doesn't put it back for a PHP worker. Keep Go workers on queues meant for them.

## Missing something?

[Open an issue](https://github.com/rjp2525/laravel-queue-go/issues) and say what you're trying to do. Several items on this page are here because nobody has needed them badly enough yet, not because they're impossible.
