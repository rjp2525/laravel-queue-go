# Worker: job outcomes are recorded during shutdown

**Breaking:** no API changes.

## What was wrong

`Delete`, `Release` and the failed-job insert ran on the context passed to
`worker.Run`. When that context was cancelled (SIGTERM in `lqworker` or any
`cancel()` in your own program), those calls failed with `context canceled`:

- a job that **finished successfully** during shutdown stayed in `:reserved`
  and ran again after `retry_after`;
- a job that failed during shutdown was not released, so its retry waited for
  `retry_after` instead of its backoff;
- a job that failed for good was removed from neither Redis nor recorded in
  `failed_jobs`.

## What changed

Job bookkeeping now uses `context.WithoutCancel`: the driver and the failed
job logger are still called after the worker's context is cancelled. Handlers
keep receiving the cancellable context, so they are still told to stop.

`job.Delete()` and `job.Release()` called from your own code behave the same
way.

## What you need to do

Nothing in most cases. Two things to be aware of:

- Shutdown can take slightly longer: after the last handler returns, the
  worker makes one more Redis call per in-flight job (and one database insert
  per job that failed for good). These calls are bounded by your Redis and
  database client timeouts, not by the cancelled context. If you run without
  client timeouts, set them (`redis.Options.ReadTimeout` / `WriteTimeout`,
  the database driver's equivalent).
- A custom `queue.Driver` or `failed.Logger` now receives a context that is
  never cancelled for `Delete`, `Release` and `Log`. If yours relied on
  cancellation to abort, give those operations their own timeout.
