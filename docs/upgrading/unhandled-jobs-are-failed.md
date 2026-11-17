# Worker: jobs without a handler are failed, not dropped

**Breaking:** behaviour change for jobs the worker cannot process. Builds on
[ack-after-cancellation](ack-after-cancellation.md).

## What was wrong

Two paths deleted a job from Redis and left no record of it:

- **No handler registered** for the job class and no default handler: the
  worker logged a warning and deleted the job. A Go worker pointed at a queue
  that also carries PHP-only jobs silently destroyed them.
- **Payload could not be parsed:** logged and deleted. The raw payload was
  gone.

## What changed

Both are now *failed* jobs, as an unresolvable job class is in Laravel:

- the job is removed from the queue (it is not retried or released, which
  would only bounce it between workers);
- it is written to the failed job provider with the original payload, so
  `php artisan queue:retry` can put it back once a worker that understands it
  is running;
- `events.JobFailed` fires. For a missing handler the error wraps
  `queue.ErrNoHandler`. For an unparseable payload the event has no job name
  or UUID, only the queue and the parse error.

## What you need to do

1. **Configure a failed job provider** if you have not. With the default
   `failed.NullProvider` the job is still removed and only the event and the
   log line remain:

   ```go
   provider, _ := failed.NewDatabaseProvider(db, "failed_jobs")
   mgr.SetFailedProvider(provider)
   ```
2. **Expect new rows in `failed_jobs`** if a Go worker shares a queue with
   job classes it has no handler for. The better fix is to route those jobs
   to a queue only Laravel workers consume.
3. **If dropping unknown jobs was intentional**, say so with a default
   handler:

   ```go
   mgr.RegisterDefault(func(ctx context.Context, job *queue.Job) error {
       return nil // acknowledge and discard
   })
   ```
4. **`JobFailed` listeners** must tolerate an event with empty `JobUUID` and
   `JobName` (unparseable payload).
