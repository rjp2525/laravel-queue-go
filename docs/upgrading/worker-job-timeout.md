# Worker: job timeouts are enforced

**Breaking:** behaviour change for jobs that carry a `timeout`. Builds on
[worker-panic-recovery](worker-panic-recovery.md).

## What was wrong

The payload's `timeout` and `failOnTimeout` were parsed and then ignored. A
job dispatched from Laravel with `public $timeout = 120` could run forever in
a Go worker. The only tool was `middleware.Timeout`, which stops *waiting* for
the handler but cannot stop it: the handler keeps running in a leaked
goroutine while the job has already been released, so the same job runs twice
at once.

## What changed

- The worker derives the handler's `ctx` with a deadline when the job has a
  timeout: the payload's `timeout` (seconds) or the new
  `WorkerOptions.Timeout` for payloads without one. A payload `timeout` of
  `0` disables it.
- A handler that returns an error after its deadline fails the attempt with a
  `*queue.TimeoutError` (which also wraps the handler's own error).
- With `failOnTimeout` in the payload, a timed-out job is failed immediately
  instead of retried, like Laravel.
- A handler that finishes **without** error after the deadline counts as a
  success: the work is done, so the job is acknowledged.
- `middleware.Timeout` is deprecated.

## What you need to do

1. **Make handlers honour `ctx`.** The timeout is cooperative: Go cannot kill
   a goroutine. Pass `ctx` to database calls, HTTP requests and Redis
   commands and check `ctx.Err()` in long loops. A handler that ignores `ctx`
   behaves as before (no timeout).
2. **Check jobs that already carry a `timeout`.** Handlers that do honour
   `ctx` will now be interrupted at that timeout. If a Go handler legitimately
   needs longer than the PHP job's `$timeout`, raise the value on the job.
3. **Remove `middleware.Timeout`** from `mgr.Use(...)` / `worker.Use(...)`.
   If you used it with a custom getter to give jobs a default timeout, use
   `WorkerOptions.Timeout`:

   ```go
   // before
   mgr.Use(middleware.Timeout(func(middleware.Job) *time.Duration { d := 60 * time.Second; return &d }))

   // after
   worker := mgr.Worker(queue.WorkerOptions{Queue: "default", Timeout: 60 * time.Second})
   ```
4. **Keep `timeout` below `retry_after`.** As with Laravel, a job that runs
   longer than `retry_after` is handed to another worker while it is still
   running.

`WorkerOptions.Timeout` defaults to 0 (no timeout), which is the previous
behaviour. Laravel's `queue:work` defaults to `--timeout=60`.

One difference from Laravel remains: PHP kills the worker process at the
timeout. A Go handler that never returns cannot be killed and will occupy its
worker slot until it does.
