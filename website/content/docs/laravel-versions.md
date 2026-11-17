---
title: Laravel versions
description: Laravel 10, 11, 12 and 13 are supported. Here's what differs between them and how the right one gets picked.
---

The queue protocol itself, meaning the Redis keys and the Lua scripts that move jobs between them, is identical from Laravel 10 through 13. A Go worker consumes jobs from any of them without being told which.

Three things do differ, and they matter mostly when you **dispatch** from Go or connect for the first time:

| | 10 | 11 | 12 | 13 |
|---|---|---|---|---|
| Default Redis prefix for `APP_NAME=Laravel` | `laravel_database_` | `laravel_database_` | `laravel-database-` | `laravel-database-` |
| Payload has `createdAt`, `delay`, `data.batchId` | | | yes | yes |
| Payload has `countCrashesAsExceptions`, `deleteWhenMissingModels` | | | | yes |
| Keys on Redis Cluster | `queues:name` | `queues:name` | `queues:name` | `queues:{name}` |

## Telling it which version you run

Set it on the driver and everything built from that driver follows:

```go
driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
    LaravelVersion: queue.Laravel12,
})
```

That picks the payload format dispatchers write, the default prefix when you don't set `Prefix`, and the cluster key layout.

Other places you can set it:

```go
queue.NewDispatcher(driver, queue.ForLaravel(queue.Laravel11)) // one dispatcher
```

```bash
LARAVEL_VERSION=12 ./worker            # via config.FromEnv
lqworker --laravel-version=12          # the bundled binary
```

`queue.ParseLaravelVersion` accepts `"12"`, `"12.x"`, `"v12.3.1"` and `"auto"`, so you can feed it whatever your deploy tooling has.

## Letting it work it out

Leave the version unset (`queue.LaravelAuto`) and a dispatcher detects it. Before its first dispatch it peeks at one job already on the queue and reads the version off the payload's keys. The answer is cached.

If the queue is empty there's nothing to learn from, so the dispatcher writes the newest format and looks again later. That's safe: Laravel ignores payload keys it doesn't recognise, so a Laravel 10 worker runs a job written in the Laravel 13 format without complaint.

You can run the detection yourself:

```go
v, err := driver.DetectLaravelVersion(ctx, "default") // LaravelAuto if the queue is empty
v, ok := queue.DetectLaravelVersion(payloadBytes)      // from any payload you have
v := job.LaravelVersion()                              // inside a handler
```

### What detection can and can't tell

Detection reads keys in the payload, so it has honest limits:

- **Laravel 10 and 11 look the same** unless the job carries a Context (`Context::add(...)`). An 11 payload without one is reported as 10. The two versions use the same payload format and prefix, so this changes nothing in practice.
- **Custom payload keys can confuse it.** If your app adds keys with `Queue::createPayloadUsing`, or runs a very early 12.x or 13.x release, detection may land one version low. Pin the version and the question goes away.
- **The prefix can't be detected.** You need the prefix to find the queue in the first place. It follows the version you configure, not the one detected.

> [!TIP]
> Auto-detection is a convenience for getting started. In production, set `LaravelVersion` explicitly. It's one line, and it removes a moving part.

## The prefix, again

It's the single most common setup problem, so once more: the default prefix lives in your app's `config/database.php`, and Laravel changed the default in version 12 from underscores to hyphens. An app created on 10 and upgraded to 13 still has `laravel_database_`.

`LaravelVersion.RedisPrefix` gives you what a fresh app of that version would use:

```go
queue.Laravel11.RedisPrefix("Acme Shop") // "acme_shop_database_"
queue.Laravel13.RedisPrefix("Acme Shop") // "acme-shop-database-"
```

When in doubt, set `Prefix` to the exact string from `config('database.redis.options.prefix')` and stop thinking about it.

## Redis Cluster

On a cluster, all keys of a queue must hash to the same slot, or the Lua scripts fail with `CROSSSLOT`.

- **Laravel 13** handles this for you by storing `queues:{default}`. Set `LaravelVersion: queue.Laravel13` and pass a `*redis.ClusterClient`; the driver uses the same layout.
- **Laravel 10 to 12** leave it to you: name the queue with a hash tag, `{default}`, in both Laravel and Go.

If you're on 13 with a cluster but leave the version on auto, set `HashTagQueues: true` so the keys match from the first call.

## Upgrading Laravel

Bumping your app from one major to the next needs nothing on the Go side as long as the prefix stays the same, which it does unless you change `config/database.php`. Update `LaravelVersion` when you get to it so dispatched payloads carry the newer keys.
