---
title: Handlers and job data
description: Reading what your Laravel job carries, from scalars to enums and Eloquent models.
---

A handler is a function:

```go
type Handler func(ctx context.Context, job *queue.Job) error
```

Return `nil` and the job is done. Return an error and it's [retried or failed](/docs/retries/).

## How a PHP job becomes Go data

When Laravel queues a job it runs `serialize()` on the job object. The worker decodes that and gives you the object's **properties** by name.

```php
class ProcessOrder implements ShouldQueue
{
    public function __construct(
        public int $orderId,
        public string $currency,
        public float $total,
        public bool $express,
        public array $lineItems,
        protected string $note = '',
    ) {}
}
```

```go
mgr.Register(`App\Jobs\ProcessOrder`, func(ctx context.Context, job *queue.Job) error {
    orderID := job.GetInt("orderId")       // int64
    currency := job.GetString("currency")  // string
    total := job.GetFloat("total")         // float64
    express := job.GetBool("express")      // bool
    items := job.GetSlice("lineItems")     // []any
    note := job.GetString("note")          // protected and private work too
    // ...
})
```

The accessors never panic. A missing property or one of the wrong type gives you the zero value, so check for it when a value is required:

```go
if orderID == 0 {
    return errors.New("job has no orderId")
}
```

> [!NOTE]
> Property visibility doesn't matter. `public`, `protected` and `private` properties are all read by their plain name.

## Arrays

PHP has one array type; Go has two. Use the accessor that matches how the array is used:

```go
tags := job.GetSlice("tags")       // ['a', 'b']            -> []any{"a", "b"}
meta := job.GetMap("metadata")     // ['source' => 'api']   -> map[string]any{"source": "api"}
```

Integers inside arrays arrive as `int64`, floats as `float64`. Nested PHP arrays arrive as `*phpserialize.Array`, which keeps PHP's key order:

```go
for _, item := range job.GetSlice("lineItems") {
    line, ok := item.(*phpserialize.Array)
    if !ok {
        continue
    }
    fields := line.ToMap()
    sku, _ := fields["sku"].(string)
    qty, _ := fields["qty"].(int64)
}
```

## Eloquent models

Jobs that use `SerializesModels` don't carry the model. Laravel stores a small pointer instead (the class, the primary key and the connection) and re-fetches the row when the job runs. In Go you get that pointer:

```php
public function __construct(public User $user) {}
```

```go
ref := job.GetModelID("user")
if ref == nil {
    return errors.New("job has no user")
}

ref.Class      // "App\Models\User"
ref.ID         // int64(42), or a string for UUID keys
ref.Connection // "mysql"
ref.Relations  // relations that were loaded when the job was dispatched
```

Loading the row is up to you. Query your database with `ref.ID`, or call an API. For a collection of models, `ref.ID` is a `*phpserialize.Array` holding the keys.

> [!TIP]
> If the Go handler only needs a couple of columns, pass them as plain constructor arguments instead of the model. It saves a query and keeps the job independent of your schema.

## Enums

A backed or pure PHP enum property comes through with its case name:

```php
public function __construct(public OrderStatus $status) {}
```

```go
job.GetString("status") // "Shipped" (the case name, not the backing value)

e := phpserialize.GetEnum(job.Data(), "status")
e.ClassName // "App\Enums\OrderStatus"
e.CaseName  // "Shipped"
```

PHP serializes the case, not its backing value, so `'shipped'` or `3` is not in the payload. Map case names to whatever your Go code uses.

## Job metadata

Everything Laravel knows about the job is on the `Job`:

```go
job.UUID()          // unique id, same one Laravel logs and stores in failed_jobs
job.DisplayName()   // usually the class name
job.CommandName()   // the class name
job.Queue()         // queue it was popped from
job.Attempts()      // 1 on the first run, 2 on the first retry, ...
job.MaxTries()      // *int, nil if the job has no $tries
job.Timeout()       // *int seconds, nil if the job has no $timeout
job.Tags()          // Horizon tags, if any
job.BatchID()       // set when the job belongs to a batch
job.LaravelVersion() // which Laravel wrote the payload
```

`job.Data()` returns the whole decoded object if you'd rather walk it yourself, and `job.Payload()` is the raw envelope.

## Taking control of the outcome

Normally you just return. If you need to, you can settle the job yourself:

```go
job.Release(30 * time.Second) // put it back, try again in 30s (counts as an attempt)
job.Delete()                  // remove it from the queue
```

To release from a handler without the retry rules getting involved, return a `ReleaseError`:

```go
return &middleware.ReleaseError{Delay: time.Minute}
```

The job goes back on the queue for a minute regardless of `$backoff`, and it is not failed even if it was on its last attempt.

## JSON commands

Jobs dispatched by this package with `queue.WithCommandFormat(queue.FormatJSON)` carry a JSON object instead of a PHP-serialized one. The same accessors work, and `job.Format()` tells you which you got. Laravel can't run those jobs, so only use the JSON format between Go programs.
