# Queue: Size() counts what Laravel counts

**Breaking:** `RedisDriver.Size` returns a larger number when a queue has
delayed or reserved jobs.

## What changed

- `RedisDriver.Size(ctx, queue)` now returns ready + delayed + reserved jobs,
  using the same script as Laravel's `Queue::size()` (and so the number
  `Queue::size()`, `queue:monitor` and Horizon show). It used to return only
  the ready list's length.
- New `PendingSize`, `DelayedSize` and `ReservedSize`, matching the methods
  Laravel 12 added.
- The migrate script pushes due jobs in chunks of 100 per `RPUSH` instead of
  one call per job and returns a count instead of sending every migrated
  payload back to the client. No behaviour change. Fewer Redis calls and less
  network traffic when many delayed jobs become due at once.
- A negative `RedisDriverConfig.BatchSize` migrates without a limit, which is
  Laravel's own default (`migration_batch_size = -1`). The default here stays
  100.

## What you need to do

Replace `Size` with `PendingSize` where you meant "jobs waiting to run now":

```go
// before
n, _ := driver.Size(ctx, "default")        // ready jobs only

// after
n, _ := driver.PendingSize(ctx, "default") // ready jobs only
all, _ := driver.Size(ctx, "default")      // ready + delayed + reserved, as in Laravel
```

Typical places: autoscaling on queue depth, "is the queue empty" checks in
tests and scripts, dashboards. An idle queue with jobs scheduled for later now
reports a non-zero `Size`.

`Size` is part of the `queue.Driver` interface. Custom drivers should follow
the same definition. The three new methods are on `*RedisDriver` only, so
custom drivers are not required to implement them.
