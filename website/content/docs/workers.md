---
title: Workers
description: Starting a worker, running jobs concurrently, limits, and shutting down without losing work.
---

A worker pulls jobs off one queue and hands each to the handler registered for its class. It's the Go counterpart of `php artisan queue:work`.

## Manager or standalone

Most programs use a `Manager`. It holds your handlers, middleware, event listeners and failed-job storage, and stamps out workers that share them:

```go
mgr := queue.NewManager(driver)
mgr.Register(`App\Jobs\ResizeImage`, resizeImage)
mgr.Register(`App\Jobs\TranscodeVideo`, transcodeVideo)
mgr.SetFailedProvider(failedJobs)
mgr.SetLogger(logger)

images := mgr.Worker(queue.WorkerOptions{Queue: "images", Concurrency: 8})
videos := mgr.Worker(queue.WorkerOptions{Queue: "videos", Concurrency: 2})
```

A worker takes a copy of the manager's handlers when it's created, so register everything first.

If you only need one worker, skip the manager:

```go
worker := queue.NewWorker(driver, queue.WorkerOptions{Queue: "images"})
worker.Register(`App\Jobs\ResizeImage`, resizeImage)
```

## Options

```go
queue.WorkerOptions{
    Queue:       "default",        // the queue to consume
    Concurrency: 4,                // jobs running at once; 0 or 1 runs them one by one
    Sleep:       3 * time.Second,  // pause when the queue is empty
    MaxJobs:     0,                // stop after this many jobs (0 = never)
    MaxTime:     0,                // stop after this many seconds (0 = never)
    StopOnEmpty: false,            // stop when the queue is empty
    MaxTries:    0,                // attempts for jobs without $tries (0 = unlimited)
    Backoff:     0,                // retry delay for jobs without $backoff
    Timeout:     0,                // time limit for jobs without $timeout (0 = none)
}
```

`MaxTries`, `Backoff` and `Timeout` are fallbacks for jobs that don't set their own, the same role `--tries`, `--backoff` and `--timeout` play for `queue:work`. They're covered in [Retries and timeouts](/docs/retries/).

> [!WARNING]
> `MaxTries` defaults to `0`, which means a job with no `$tries` is retried forever. Laravel's `queue:work` defaults to one attempt. Set `MaxTries: 1` if you want the Laravel behaviour, and set it to *something* in production.

## Concurrency

With `Concurrency: 8` the worker runs up to eight handlers at once, each in its own goroutine. It only reserves a job when a slot is free, so a worker never holds more jobs than it can run. Handlers must be safe to run concurrently with each other.

How high should it go? For I/O-bound jobs (HTTP calls, database writes), tens or hundreds is normal, limited by what the systems you call can take. For CPU-bound jobs, start at the number of cores.

One worker consumes one queue. To cover several, run several workers in the same process:

```go
var wg sync.WaitGroup
for _, w := range []*queue.Worker{images, videos} {
    wg.Go(func() { _ = w.Run(ctx) })
}
wg.Wait()
```

## Blocking pop and polling

When the queue is empty the worker sleeps for `Sleep` and looks again. To pick up new jobs immediately, set `BlockFor` on the driver. The worker then waits on Redis and is woken the instant a job is pushed:

```go
driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
    BlockFor: 5 * time.Second,
})
```

This is Laravel's `block_for`. Keep it at a few seconds: it also bounds how quickly a worker notices it has been asked to stop.

## Shutting down

There are two ways to stop a worker, and the difference matters on every deploy.

**`worker.Stop()`** stops taking new jobs and lets the ones in flight finish. `Run` returns once they have.

**Cancelling the context** you passed to `Run` also stops the worker, and additionally cancels the `ctx` every running handler holds.

The pattern you want in production is both, in order: drain first, and only cut jobs off if they overstay.

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

signals := make(chan os.Signal, 2)
signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)

go func() {
    <-signals
    worker.Stop() // finish what's running, take nothing new

    select {
    case <-signals: // asked twice: stop now
    case <-time.After(25 * time.Second):
    }
    cancel()
}()

err := worker.Run(ctx)
```

Either way, a job's outcome is always recorded. If a handler returns during shutdown, the job is still acknowledged, released or written to `failed_jobs`, even though the context is already cancelled.

> [!NOTE]
> Set your orchestrator's grace period longer than your drain timeout. Kubernetes gives a pod 30 seconds by default, so a 25 second drain fits. If the process is killed mid-job anyway, the job is not lost: it stays reserved and is retried after `retry_after`.

## What a worker does with a job it can't run

- **No handler for the class** and no default handler: the job is failed with `queue.ErrNoHandler` and written to your failed job storage. It is not dropped.
- **Unparseable payload:** same, with the raw payload preserved so you can look at it.
- **Handler panics:** the panic is recovered and treated as a failed attempt. The worker and its other jobs carry on. The error is a `*queue.PanicError` with the stack trace.

To handle every class you haven't registered, add a default handler:

```go
mgr.RegisterDefault(func(ctx context.Context, job *queue.Job) error {
    return fmt.Errorf("unexpected job %s", job.DisplayName())
})
```

> [!WARNING]
> Don't point a Go worker at a queue that also carries jobs meant for PHP. It will reserve them, find no handler, and fail them. Give Go its own queues.
