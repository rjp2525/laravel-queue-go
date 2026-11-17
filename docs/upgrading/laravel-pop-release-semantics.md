# Queue: Laravel's reserve/release semantics

**Breaking:** yes, for code that reads `job.Attempts()` and for custom
`queue.Driver` implementations.

## What was wrong

- `Job.Release` re-encoded the payload with `attempts+1` and then asked Redis
  to remove *that* string from `queues:{name}:reserved`. The reserved set held
  the original string, so nothing was removed: every released job stayed in
  `:reserved` as well and was delivered a second time after `retry_after`.
- The re-encoding dropped every payload key this package does not model
  (`silenced`, `data.batchId`, `telescope_uuid`, ...).
- `attempts` was only incremented on release. A job whose worker crashed went
  back to the queue with the same count, so it could be retried forever, and
  a job with `maxTries: 3` ran four times.
- With `BlockFor` unset the `:notify` list was never consumed and grew by one
  entry per pushed job. With `BlockFor` set, the worker waited on `:notify`
  before looking at the queue, so a ready job could sit for `BlockFor`.
- `Later` pushed a notification for a job that was not ready yet, waking
  blocked workers (Go and Laravel) for nothing.

## What changed

The driver now runs Laravel's own `pop` script (identical in Laravel 10, 11,
12 and 13): `attempts` is incremented inside Redis when the job is reserved,
the reserved copy is what `Delete`/`Release` address, and each pop consumes
one notification.

| | Before | After |
|---|---|---|
| `job.Attempts()` on the first run | `0` | `1` |
| `events.Event.Attempt` on the first run | `0` | `1` |
| Runs of an always-failing job with `maxTries: 3` | 4 | 3 |
| `job.Release(d)` | rewrites `Raw().Body`, bumps `Attempts()` | moves the reserved payload untouched |
| `job.Raw().Body` | payload as popped, rewritten on release | payload as pushed, never rewritten |
| `job.Raw().Reserved` | n/a | payload held in `:reserved` (attempts incremented) |
| `Pop` with `BlockFor` | BLPOP, then pop | pop, BLPOP only when empty, pop again |
| BLPOP failure | ignored | returned from `Pop` |

## What you need to do

1. **Handlers and listeners that use the attempt number.** `Attempts()` is now
   1-based, like `$this->attempts()` in a Laravel job. Change `== 0` checks
   for "first run" to `== 1`, and drop any `+1` you added for display.
2. **Custom `queue.Driver` implementations.** `Pop` must now increment the
   payload's `attempts` itself and return that payload in `RawJob.Reserved`
   (keep the original in `Body`). `Job.Release` no longer increments for you.
   If `Reserved` is left empty, `Body` is used for `Delete`/`Release` and the
   attempt count will not advance.
3. **Reserved payloads round-trip through Redis' cjson**, exactly as they do
   under a Laravel worker: an empty array is reserved as `{}` (so `"tags":[]`
   becomes `"tags":{}`) and key order changes. `LaravelPayload` parsing accepts
   this; if you parse `Raw().Reserved` yourself, do the same. `Raw().Body` is
   not re-encoded.
4. **One-time cleanup (optional).** Queues that ran the previous version may
   hold leftovers:
   - `{prefix}queues:{name}:reserved` can contain stale copies of jobs that
     were released. They will be delivered once more after `retry_after`. With
     workers stopped you can inspect and remove them with `ZRANGE` / `ZREM`.
   - `{prefix}queues:{name}:notify` may be long. It only holds wake-up
     tokens; `DEL` it at any time.

No change is needed on the Laravel side.
