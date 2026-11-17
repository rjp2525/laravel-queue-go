---
title: Dispatching jobs
description: Push jobs from Go for Laravel's workers, or your own, to run.
---

A dispatcher puts jobs on a queue. To Laravel they look exactly like jobs it dispatched itself, so `php artisan queue:work` runs them without knowing where they came from.

```go
d := mgr.Dispatcher() // or queue.NewDispatcher(driver)

err := d.Dispatch(ctx, `App\Jobs\SendInvoice`, map[string]any{
    "invoiceId": 9001,
    "email":     "billing@example.com",
})
```

## How arguments reach PHP

The map becomes the PHP object's **properties**. Laravel rebuilds the job with `unserialize()`, which sets properties directly and never calls the constructor.

```php
class SendInvoice implements ShouldQueue
{
    use Queueable;

    public int $invoiceId;
    public string $email;

    public function handle(Mailer $mailer): void
    {
        // $this->invoiceId and $this->email are set
    }
}
```

Three things follow from that:

- **Keys must match property names exactly**, including case.
- **Properties must be public.** A key with no matching public property becomes a dynamic property, which PHP 8.2+ warns about.
- **Types must fit.** A Go `int` arrives as a PHP `int`, `float64` as `float`, `string`, `bool`, slices and maps as arrays, `nil` as `null`. Sending `"42"` to an `int` property is a `TypeError`.

> [!TIP]
> Models can't be sent from Go, but their keys can. Dispatch `"userId": 42` and call `User::find($this->userId)` in `handle()`.

## Delays

```go
d.Later(ctx, 15*time.Minute, `App\Jobs\SendReminder`, map[string]any{"userId": 7})
```

The job sits in the delayed set and becomes available after 15 minutes, the same as `->delay(now()->addMinutes(15))`.

## Options

Per-job settings mirror the properties you'd put on the class:

```go
d.Dispatch(ctx, `App\Jobs\ImportFeed`, args,
    queue.WithMaxTries(5),
    queue.WithBackoff([]int{10, 60, 300}), // seconds; time.Duration works too
    queue.WithTimeout(120),                // seconds
    queue.WithFailOnTimeout(true),
    queue.WithRetryUntil(time.Now().Add(time.Hour).Unix()),
    queue.WithTags("imports", "feed:42"),
)
```

Set these here even if the PHP class already declares `$tries` or `$backoff`. Laravel copies those properties into the payload when *it* dispatches a job, and its worker only reads the payload. A job dispatched from Go never passes through that step, so the class's own values are not picked up.

To choose the queue, or when you have many settings, use `DispatchWithOptions`:

```go
tries := 3

d.DispatchWithOptions(ctx, queue.DispatchOptions{
    Job:      `App\Jobs\ImportFeed`,
    Queue:    "imports",
    Delay:    30 * time.Second,
    MaxTries: &tries,
    Backoff:  "10,60",
    Tags:     []string{"imports"},
    Args:     map[string]any{"feedId": 42},
})
```

A dispatcher has a default queue for `Dispatch` and `Later`:

```go
d := queue.NewDispatcher(driver, queue.WithDefaultQueue("imports"))
```

## Which Laravel version to write for

Payloads differ slightly between Laravel versions. By default the dispatcher looks at a job already on the queue and writes the same format; with nothing to look at, it writes the newest format, which older versions accept. To pin it:

```go
d := queue.NewDispatcher(driver, queue.ForLaravel(queue.Laravel11))
```

Or set `LaravelVersion` once on the driver and every dispatcher follows. Details are in [Laravel versions](/docs/laravel-versions/).

## Go to Go

If both ends are Go, there's no reason to go through PHP serialization. The JSON format stores the command as a plain JSON object:

```go
d := queue.NewDispatcher(driver, queue.WithCommandFormat(queue.FormatJSON))
```

Go workers read both formats without being told. Laravel can't run JSON commands, so keep them on queues PHP doesn't consume.

## What dispatching doesn't do

- **No `ShouldBeUnique`.** Laravel takes that lock in PHP at dispatch time. A job dispatched from Go skips it.
- **No `afterCommit`.** The job is pushed immediately. If it depends on a database transaction, dispatch after you commit.
- **No transactions across Redis and your database.** If the push fails you get an error back; decide whether to retry.
