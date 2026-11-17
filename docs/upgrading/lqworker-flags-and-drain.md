# lqworker: flag parsing and graceful shutdown

**Breaking:** shutdown behaviour of the `lqworker` binary. The library is not
affected. Builds on [config-laravel-env](config-laravel-env.md).

## What was wrong

- `--sleep`, `--retry-after` and `--block-for` were documented as seconds
  ("Seconds to sleep when no jobs available") but only accepted Go durations:
  `lqworker --sleep=3` exited with `invalid value "3" for flag -sleep`.
- `SIGTERM` cancelled the context every running job was on. Jobs were
  interrupted mid-flight on every deploy, although the library has had
  `Worker.Stop()` for a graceful drain since v1.3.0.
- Invalid environment values were silently ignored.
- There was no way to pass a Redis ACL username.
- Nothing warned that the binary acknowledges and so deletes, every job it
  receives.

## What changed

- Time flags accept seconds (`--sleep=3`, `--sleep=0.5`) or durations
  (`--sleep=3s`).
- Two-stage shutdown: the first `SIGTERM`/`SIGINT` calls `Worker.Stop()`.
  Running jobs finish and the process exits. A second signal or the new
  `--shutdown-timeout` (default `0` = wait as long as it takes), cancels the
  running jobs.
- The environment is validated at startup. An invalid value prints the
  offending variables and exits with status 2.
- New `--redis-username` flag / `REDIS_USERNAME`.
- The usage text and a startup log line state that jobs are acknowledged and
  removed.

## What you need to do

1. **Process managers and orchestrators:** `lqworker` no longer exits
   immediately on `SIGTERM`. It waits for running jobs. Make sure the stop
   grace period is long enough or bound the wait:

   ```bash
   lqworker --queue=default --shutdown-timeout=25   # Kubernetes default grace is 30s
   ```

   Supervisor: `stopwaitsecs` should exceed `--shutdown-timeout`. Without a
   timeout the process is killed by the supervisor's own deadline, as before.
2. **Environment:** fix any invalid `QUEUE_*` / `REDIS_DB` values. The binary
   now refuses to start with them instead of quietly using defaults.
3. **Scripts using Go durations** (`--sleep=3s`) keep working.
