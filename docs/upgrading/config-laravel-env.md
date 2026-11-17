# config: reads a Laravel .env correctly

**Breaking:** small behaviour changes in `config.FromEnv`, all in the
direction of what a Laravel `.env` means.

## What was wrong

Pointing a Go worker at the environment of the Laravel app it works with did
not work:

| `.env` line (Laravel default) | Laravel | Before |
|---|---|---|
| `REDIS_HOST=127.0.0.1` + `REDIS_PORT=6379` | host and port | address `127.0.0.1` with no port: `dial tcp: missing port in address` |
| `REDIS_PASSWORD=null` | no password | sent `AUTH null`, which Redis rejects |
| `REDIS_QUEUE_RETRY_AFTER=90` | 90 seconds | variable not read |
| `QUEUE_RETRY_AFTER=90` | n/a | `time.ParseDuration("90")` fails, value **silently ignored** |
| `REDIS_QUEUE=emails` | queue name | variable not read |
| `QUEUE_CONCURRENCY=four` (typo) | n/a | silently ignored |

## What changed

- `REDIS_HOST` may be a bare host. `REDIS_PORT` (default 6379) is appended.
  `host:port` in `REDIS_HOST` still works and wins over `REDIS_PORT`. IPv6
  hosts are bracketed.
- New `Config.RedisUsername` from `REDIS_USERNAME`.
- Values are read like Laravel's `env()`: surrounding quotes are removed and
  `null` / `(null)` / `empty` / `(empty)` count as unset.
- `REDIS_QUEUE` and `REDIS_QUEUE_RETRY_AFTER` are fallbacks for `QUEUE_NAME`
  and `QUEUE_RETRY_AFTER`.
- Durations (`QUEUE_RETRY_AFTER`, `QUEUE_BLOCK_FOR`, `QUEUE_SLEEP`) accept a
  bare number of seconds as well as Go syntax. New `config.ParseDuration`.
- New `config.Load() (*Config, error)`: same result as `FromEnv`, plus an
  error naming each variable that could not be parsed.

## What you need to do

1. **If you set `REDIS_PASSWORD` to the literal string `null`, `empty`,
   `(null)` or `(empty)`** as an actual password: it is now read as "no
   password". Set `Config.RedisPassword` in code instead.
2. **If your environment has `REDIS_PORT`, `REDIS_QUEUE` or
   `REDIS_QUEUE_RETRY_AFTER` set** (typically because it is shared with
   Laravel) they now take effect when the corresponding Go variable is not
   set. Before, `REDIS_HOST=10.0.0.5` + `REDIS_PORT=6380` could never have
   worked, so this only matters if you relied on those variables being
   ignored.
3. **`QUEUE_RETRY_AFTER=90`-style values now apply.** They used to be
   dropped, leaving the 90s / 5s / 3s defaults. Check that the values in your
   environment are the ones you want.
4. **Pass the username on** if you build the Redis client yourself:

   ```go
   cfg, err := config.Load()
   if err != nil {
       log.Fatal(err) // e.g. QUEUE_CONCURRENCY="four" is not an integer
   }
   rdb := redis.NewClient(&redis.Options{
       Addr:     cfg.RedisAddr,
       Username: cfg.RedisUsername,
       Password: cfg.RedisPassword,
       DB:       cfg.RedisDB,
   })
   ```

`FromEnv` keeps its signature and still never fails.
