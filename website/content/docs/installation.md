---
title: Installation
description: Requirements, the go get line and the two settings that have to match your Laravel app.
---

## Requirements

| | Version |
|---|---|
| Go | 1.26 or newer |
| Redis | 6 or newer |
| Laravel | 10, 11, 12 or 13, using the `redis` queue connection |

Your Laravel app needs `QUEUE_CONNECTION=redis`. Either Redis client (`phpredis` or `predis`) is fine.

## Install

```bash
go get github.com/rjp2525/laravel-queue-go/v2
```

The `/v2` at the end is part of the module path. It's in every import too. If you're coming from v1, the [upgrade guide](/docs/upgrading/) covers the switch.

The package builds on [go-redis](https://github.com/redis/go-redis). You create the Redis client yourself and hand it over, so connection pooling, TLS, Sentinel and Cluster are configured the way you already know.

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/rjp2525/laravel-queue-go/v2/pkg/queue"
)

rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})

driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
    Prefix:     "laravel-database-",
    RetryAfter: 90 * time.Second,
})
```

## The two things that must match Laravel

Almost every "my worker starts but never sees a job" report comes down to one of these.

### 1. The Redis key prefix

Laravel prefixes every Redis key. The default comes from `config/database.php` and depends on `APP_NAME` and on which Laravel version generated that file:

| App created on | `APP_NAME=Acme Shop` gives |
|---|---|
| Laravel 10 or 11 | `acme_shop_database_` |
| Laravel 12 or 13 | `acme-shop-database-` |

An app that started on Laravel 10 and was upgraded since still has the underscore form, because the config file is yours and upgrades don't rewrite it. Don't guess. Ask the app:

```bash
php artisan tinker --execute="echo config('database.redis.options.prefix');"
```

Or ask Redis, from Go:

```go
queues, err := queue.DiscoverQueues(ctx, rdb)
// [{Prefix: "acme_shop_database_", Queue: "default"}, ...]
```

`DiscoverQueues` only finds queues that currently hold jobs, so dispatch one first.

If your app sets an empty prefix, pass `NoPrefix: true`. An empty `Prefix` on its own means "use the default".

### 2. `retry_after`

`RetryAfter` is how long a job may stay reserved before Redis hands it to another worker. It's the same setting as `retry_after` in `config/queue.php` (90 seconds by default). It should be **longer than your slowest job**. If a job takes 5 minutes and `RetryAfter` is 90 seconds, a second worker will start the same job while the first is still on it.

> [!TIP]
> Give slow jobs their own queue with a generous `RetryAfter`, rather than raising it for everything. A long `RetryAfter` also means a job whose worker crashed waits that long before it is retried.

## Check the connection

A ten-line sanity check before you write any handlers:

```go
size, err := driver.Size(ctx, "default")
if err != nil {
    log.Fatal(err)
}
fmt.Println("jobs on default:", size)
```

Dispatch a job from Laravel and run that. You should see `1`. If you see `0`, the prefix is wrong. Next: [write your first handler](/docs/quick-start/).
