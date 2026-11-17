# Queue: retry rules now follow Laravel's worker

**Breaking:** small behaviour changes in when a job is retried or failed.
No API was removed. Builds on
[laravel-pop-release-semantics](laravel-pop-release-semantics.md).

## What was wrong

| Payload | Laravel worker | This package (before) |
|---|---|---|
| `"backoff":"10,60"` (how Laravel writes it) | waits 10s, then 60s | ignored, retried immediately |
| `"maxTries":0` | unlimited attempts | failed on the first error |
| `"retryUntil"` in the future plus `"maxTries":1` | retried until the deadline | failed after one attempt |
| `backoff` dispatched from Go as `[]int{1,5,10}` | `explode()` throws a `TypeError` in the worker's failure handler | n/a |
| job reserved a 3rd time with `maxTries: 2` because the worker was killed twice | failed with `MaxAttemptsExceededException`, not run | run again, forever |

## What changed

- `backoff` is read in every shape it can take (Laravel's `"1,5,10"` string,
  a number, an array) and is always **written** as Laravel's comma-joined
  string. `WithBackoff` additionally accepts `time.Duration` and
  `[]time.Duration`.
- `maxTries: 0` means unlimited. A `retryUntil` deadline, when set, replaces
  `maxTries` entirely.
- A job that is reserved after it already used all its attempts is failed
  with `queue.ErrMaxAttemptsExceeded` without running the handler.
- New `WorkerOptions.MaxTries` and `WorkerOptions.Backoff` supply defaults
  for payloads that carry none, like `queue:work --tries` / `--backoff`.

## What you need to do

1. **Jobs that relied on `maxTries: 0` failing immediately** now retry without
   limit. Use `1` for "never retry".
2. **Jobs with both `retryUntil` and `maxTries`** are now retried until the
   deadline regardless of `maxTries`. Remove `retryUntil` if you want the
   attempt cap.
3. **Laravel-dispatched jobs with a `backoff`** are now delayed between
   retries. Nothing to change; expect retries to be slower and correct.
4. **`WorkerOptions.MaxTries` defaults to 0 (unlimited)**, which is what this
   package always did for payloads without `maxTries`. Laravel's
   `queue:work` defaults to `--tries=1`; set `MaxTries: 1` to match it:

   ```go
   worker := mgr.Worker(queue.WorkerOptions{
       Queue:    "default",
       MaxTries: 1,               // queue:work --tries=1
       Backoff:  3 * time.Second, // queue:work --backoff=3
   })
   ```
5. **If you read the raw JSON of Go-dispatched jobs**, `backoff` is now a
   string (`"1,5,10"`) instead of a number or array.
   `LaravelPayload.BackoffSeconds` handles both.
6. **Listeners on `events.JobFailed`** can now receive an error wrapping
   `queue.ErrMaxAttemptsExceeded` for jobs whose handler never ran.

Not covered: `maxExceptions` is still not enforced. Laravel keeps that
counter in the application cache (`job-exceptions:{uuid}`), which this package
does not have access to.
