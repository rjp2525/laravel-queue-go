# Laravel version selection and auto-detection (10, 11, 12, 13)

**Breaking:** mostly additive. Three behaviour changes are listed under
"What you need to do". Builds on [retry-semantics](retry-semantics.md).

## What is new

- `queue.LaravelVersion` (`LaravelAuto`, `Laravel10` ... `Laravel13`),
  `ParseLaravelVersion`, `DetectLaravelVersion`.
- `RedisDriverConfig.LaravelVersion` and `HashTagQueues`,
  `RedisDriver.DetectLaravelVersion`, `queue.DiscoverQueues`.
- Dispatcher option `queue.ForLaravel(v)`; payload options
  `WithLaravelVersion`, `WithDelay`, `WithBatchID`,
  `WithDeleteWhenMissingModels`.
- `LaravelPayload` models the keys added since Laravel 10:
  `CountCrashesAsExceptions`, `DeleteWhenMissingModels`, `CreatedAt`, `Delay`,
  `LogContext`, `Data.BatchID`.
- `job.LaravelVersion()`.
- `LARAVEL_VERSION` and `APP_NAME` environment variables,
  `lqworker --laravel-version`.

What each version changes:

| | 10 | 11 | 12 | 13 |
|---|---|---|---|---|
| Default prefix (`APP_NAME=Laravel`) | `laravel_database_` | `laravel_database_` | `laravel-database-` | `laravel-database-` |
| Payload keys written | base set | base set | + `createdAt`, `delay`, `data.batchId` | + `countCrashesAsExceptions`, `deleteWhenMissingModels` |
| Keys on a `*redis.ClusterClient` | `queues:name` | `queues:name` | `queues:name` | `queues:{name}` |

The Lua scripts and the reserve/release protocol do not depend on the version.

## What you need to do

1. **Go-dispatched payloads have more keys.** With the default `LaravelAuto`
   and an empty queue, payloads now use the Laravel 13 key set (`createdAt`,
   `delay`, `data.batchId`, `countCrashesAsExceptions`,
   `deleteWhenMissingModels`). Laravel 10-12 ignore unknown keys, so no action
   is needed for Laravel consumers. If your own tooling compares payload JSON
   byte for byte, pin the version:

   ```go
   driver := queue.NewRedisDriver(rdb, queue.RedisDriverConfig{
       Prefix:         "laravel_database_",
       LaravelVersion: queue.Laravel11,
   })
   ```
2. **`Later` / `DispatchOptions.Delay` now record `delay`** for Laravel 12 and
   13 targets, as Laravel does. Every Laravel worker uses `delay` as the retry
   backoff when the job has no `backoff` of its own, so a job delayed by 60s
   and dispatched without a backoff is retried 60s after a failure instead of
   immediately. Pass `queue.WithBackoff(0)` to keep immediate retries.
3. **`config.FromEnv` derives the prefix from `APP_NAME`.** Previously the
   prefix was `laravel-database-` unless `REDIS_PREFIX` was set. It is now
   `{slug(APP_NAME)}-database-` (or `_database_` with `LARAVEL_VERSION=10|11`),
   which is what the Laravel app itself uses. If your environment has
   `APP_NAME` set but your queues really live under `laravel-database-`, set
   `REDIS_PREFIX=laravel-database-`.
4. **Laravel 10 / 11 apps:** the built-in default prefix was wrong for you
   (`laravel-database-` instead of `laravel_database_`). If you already pass
   the correct `Prefix`, nothing changes. Otherwise set
   `LaravelVersion: queue.Laravel10` (or `Laravel11`) and drop the workaround.
5. **Laravel 13 on Redis Cluster:** set `LaravelVersion: queue.Laravel13` or
   `HashTagQueues: true`. Laravel 13 stores `queues:{default}` on cluster
   connections where earlier versions stored `queues:default`; without the
   setting Go and Laravel read different keys.
6. **Code that marshals a `LaravelPayload` built as a struct literal** gets
   the latest key set. Use `NewPayload(..., WithLaravelVersion(v))` to choose.

## Limits of auto-detection

Detection looks at payload keys of a job already on the queue.

- Laravel 11 is reported as 10 unless the job carries an
  `illuminate:log:context`. Both versions write the same payload and use the
  same prefix, so the result is equivalent.
- Applications can add payload keys (`Queue::createPayloadUsing`), and early
  12.x / 13.x releases may not write the keys that identify them yet.
- The prefix and the cluster key layout are needed to find the queue in the
  first place, so they follow the configured version, not the detected one.
  `queue.DiscoverQueues` lists the prefixes that actually exist in Redis.

Not version-aware yet: unique-job lock keys (the format changed in 11 and
again in 12) and the `WithoutOverlapping` / `ThrottlesExceptions` cache keys
(changed in 12). The middleware in this package uses its own keys and does not
share locks with Laravel.
