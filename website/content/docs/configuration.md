---
title: Configuration
description: Every setting in one place, and how to load them from the same .env your Laravel app uses.
---

Configuration is plain Go structs. There's no global state and nothing reads the environment unless you ask it to.

## The driver

```go
driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
    Prefix:         "laravel-database-",
    RetryAfter:     90 * time.Second,
    BlockFor:       5 * time.Second,
    BatchSize:      100,
    LaravelVersion: queue.Laravel12,
})
```

| Field | Default | What it does |
|---|---|---|
| `Prefix` | from `LaravelVersion` | Redis key prefix. Must match `database.redis.options.prefix` in Laravel. |
| `NoPrefix` | `false` | Use no prefix at all. Needed because an empty `Prefix` means "default". |
| `RetryAfter` | `90s` | How long a job may stay reserved before it's handed out again. Laravel's `retry_after`. |
| `BlockFor` | `0` | Wait this long on Redis for a new job instead of polling. Laravel's `block_for`. |
| `BatchSize` | `100` | How many due delayed or expired jobs to move per pass. Negative means no limit. |
| `LaravelVersion` | auto | See [Laravel versions](/docs/laravel-versions/). |
| `HashTagQueues` | `false` | Force `queues:{name}` keys for Redis Cluster. |

## The worker

All of `queue.WorkerOptions` is described on the [workers](/docs/workers/#options) page.

## Loading from the environment

`config.FromEnv` reads settings from environment variables and gives you a struct to build from. It understands the variables a Laravel `.env` already has, so a Go worker deployed next to the app can share its environment.

```go
import "github.com/rjp2525/laravel-queue-go/pkg/config"

cfg, err := config.Load()
if err != nil {
    log.Fatal(err) // QUEUE_CONCURRENCY="four" is not an integer
}

rdb := redis.NewClient(&redis.Options{
    Addr:     cfg.RedisAddr,
    Username: cfg.RedisUsername,
    Password: cfg.RedisPassword,
    DB:       cfg.RedisDB,
})

driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
    Prefix:         cfg.RedisPrefix,
    RetryAfter:     cfg.RetryAfter,
    BlockFor:       cfg.BlockFor,
    BatchSize:      cfg.MigrationBatchSize,
    LaravelVersion: cfg.LaravelVersion,
})

worker := mgr.Worker(queue.WorkerOptions{
    Queue:       cfg.DefaultQueue,
    Concurrency: cfg.Concurrency,
    MaxJobs:     cfg.MaxJobs,
    MaxTime:     cfg.MaxTime,
    Sleep:       cfg.Sleep,
})
```

`config.Load` tells you about values it couldn't parse. `config.FromEnv` returns the same struct and silently keeps the default for anything invalid; use it when you'd rather start than stop.

### Variables

| Variable | Default | Notes |
|---|---|---|
| `REDIS_HOST` | `127.0.0.1` | A host, or `host:port` |
| `REDIS_PORT` | `6379` | Used when `REDIS_HOST` has no port |
| `REDIS_USERNAME` | | Redis ACL user |
| `REDIS_PASSWORD` | | `null` is read as "no password", as Laravel does |
| `REDIS_DB` | `0` | |
| `REDIS_PREFIX` | derived | See below |
| `APP_NAME` | | Used to derive the prefix |
| `LARAVEL_VERSION` | `auto` | `10`, `11`, `12`, `13` or `auto` |
| `QUEUE_NAME` | `default` | Falls back to Laravel's `REDIS_QUEUE` |
| `QUEUE_RETRY_AFTER` | `90s` | Falls back to `REDIS_QUEUE_RETRY_AFTER` |
| `QUEUE_BLOCK_FOR` | `5s` | |
| `QUEUE_SLEEP` | `3s` | |
| `QUEUE_CONCURRENCY` | `1` | |
| `QUEUE_MAX_JOBS` | `0` | `0` means no limit |
| `QUEUE_MAX_TIME` | `0` | Seconds; `0` means no limit |
| `QUEUE_MIGRATION_BATCH_SIZE` | `100` | |
| `QUEUE_FAILED_TABLE` | `failed_jobs` | |
| `DATABASE_URL` | | A DSN for you to open; nothing connects automatically |

Durations accept Go syntax (`90s`, `1m30s`, `500ms`) or a bare number of seconds (`90`), which is how Laravel writes them.

### How the prefix is derived

If `REDIS_PREFIX` is set, that's the prefix. If not, it's built the way Laravel builds its default: a slug of `APP_NAME` followed by `_database_` (Laravel 10 and 11) or `-database-` (12, 13 and auto).

```bash
APP_NAME="Acme Shop" LARAVEL_VERSION=11   # acme_shop_database_
APP_NAME="Acme Shop"                      # acme-shop-database-
```

> [!WARNING]
> That derivation matches a *fresh* app. If your `config/database.php` has been edited, or the app predates Laravel 12 and was upgraded, the real prefix may differ. Setting `REDIS_PREFIX` explicitly, in both Laravel and Go, is the reliable choice.

## Logging

Workers log through `log/slog`. They use the default logger unless you give them one:

```go
mgr.SetLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
```

The worker logs problems it can't hand back to you: a failed Redis call, a failed-jobs insert that errored, a job that failed permanently. It does not log every job. Use [events](/docs/events/) for that.
