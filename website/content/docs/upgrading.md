---
title: Upgrading
description: Moving from v1 to v2. What you need to touch and what you can leave alone.
---

v2 is the result of a full audit against Laravel 10 through 13. It fixes bugs that duplicated or lost jobs. A few of those fixes change behaviour you may have coded around.

Most apps need four small edits. Start there.

## The four you'll probably hit

### 1. The import path ends in `/v2`

Go puts the major version in the module path, so v2 is a different import path from v1:

```bash
go get github.com/rjp2525/laravel-queue-go/v2
```

```go
// before
import "github.com/rjp2525/laravel-queue-go/pkg/queue"

// after
import "github.com/rjp2525/laravel-queue-go/v2/pkg/queue"
```

A find-and-replace of `laravel-queue-go/pkg/` with `laravel-queue-go/v2/pkg/` across your project does it. Then run `go mod tidy` to drop the v1 requirement.

> [!NOTE]
> Use **v2.0.1 or newer**. The v2.0.0 tag was published without the `/v2` path and can't be installed. `go get` rejects it.

### 2. `job.Attempts()` starts at 1

It used to be `0` on the first run. It now matches `$this->attempts()` in Laravel.

```go
// before
if job.Attempts() == 0 { /* first run */ }

// after
if job.Attempts() == 1 { /* first run */ }
```

The same goes for `events.Event.Attempt`. As a side effect, a job with `$tries = 3` now runs three times, not four.

### 3. `Size` counts everything

`driver.Size` used to count only jobs ready to run. It now counts ready, delayed and reserved jobs, like `Queue::size()` in Laravel.

```go
// before
n, _ := driver.Size(ctx, "default")

// after, same meaning as before
n, _ := driver.PendingSize(ctx, "default")
```

### 4. Jobs without a handler are failed

They used to be deleted with a log line. They're now written to failed job storage and announced with `JobFailed`. If you were relying on unknown jobs being dropped, say so:

```go
mgr.RegisterDefault(func(ctx context.Context, job *queue.Job) error {
    return nil // acknowledge and discard
})
```

## Behaviour that changed for the better

You don't have to do anything about these, but you'll notice them.

| What | Before | Now |
|---|---|---|
| A released job | Delivered twice | Delivered once |
| `$backoff` on Laravel-dispatched jobs | Ignored | Honoured |
| `$tries = 0` | Failed immediately | Unlimited, as in Laravel |
| `retryUntil` with `$tries` | Whichever came first | `retryUntil` wins, as in Laravel |
| `$timeout` | Ignored | Cancels the handler's `ctx` |
| Handler panic | Crashed the process | Counted as a failed attempt |
| Job finishing during shutdown | Ran again later | Acknowledged |
| Worker killed mid-job, repeatedly | Retried forever | Failed after `$tries` |
| Failed jobs on PostgreSQL | Never recorded | Recorded |

If your retries used to be instant and are now slower, that's `$backoff` finally working.

## If you use these, read the guide

Each links to a short guide in the repository with before-and-after code.

| You use | What changed | Guide |
|---|---|---|
| A custom `queue.Driver` | `Pop` must count the attempt and fill `RawJob.Reserved` | [reserve and release](https://github.com/rjp2525/laravel-queue-go/blob/main/docs/upgrading/laravel-pop-release-semantics.md) |
| `batch.Repository` | Uses Laravel's real schema. `Options` is PHP-serialized | [batches](https://github.com/rjp2525/laravel-queue-go/blob/main/docs/upgrading/batch-laravel-schema.md) |
| `DispatchOptions.Chain` | Chained entries are serialized commands | [chains](https://github.com/rjp2525/laravel-queue-go/blob/main/docs/upgrading/chain-dispatch-format.md) |
| `middleware.Timeout` | Deprecated. The worker enforces `$timeout` | [timeouts](https://github.com/rjp2525/laravel-queue-go/blob/main/docs/upgrading/worker-job-timeout.md) |
| `events.WorkerStopping` | Fires once, after in-flight jobs drain | [worker accounting](https://github.com/rjp2525/laravel-queue-go/blob/main/docs/upgrading/worker-concurrency-accounting.md) |
| `config.FromEnv` | Reads Laravel's `.env` conventions. Derives the prefix from `APP_NAME` | [config](https://github.com/rjp2525/laravel-queue-go/blob/main/docs/upgrading/config-laravel-env.md) |
| The `lqworker` binary | Drains on `SIGTERM`. Validates its environment | [lqworker](https://github.com/rjp2525/laravel-queue-go/blob/main/docs/upgrading/lqworker-flags-and-drain.md) |
| Laravel 10 or 11 | Now supported. The default prefix follows the version | [versions](https://github.com/rjp2525/laravel-queue-go/blob/main/docs/upgrading/laravel-version.md) |

Every guide is in [`docs/upgrading`](https://github.com/rjp2525/laravel-queue-go/tree/main/docs/upgrading).

## One-time cleanup

Queues that ran v1.3.0 or earlier may carry leftovers from the release bug:

- **`{prefix}queues:{name}:reserved`** can hold stale copies of jobs that were released. Each is delivered once more after `retry_after`. If that's a problem, stop your workers, inspect the set with `ZRANGE` and remove what shouldn't be there with `ZREM`.
- **`{prefix}queues:{name}:notify`** may be very long. It holds wake-up tokens and nothing else. `DEL` it whenever you like.

## While you're in there

Since you're touching the worker anyway:

- Set `MaxTries` on your workers. It still defaults to unlimited.
- Set `LaravelVersion` on the driver.
- Configure a failed job provider if you haven't.

The [production checklist](/docs/going-to-production/) has the rest.
