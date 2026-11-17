# middleware: lock ownership and sub-second rate-limit windows

**Breaking:** no API changes. The value stored under lock keys changed.

## What was wrong

- `WithoutOverlapping` and `Unique` released their lock with an unconditional
  `DEL`. If a job ran longer than the lock's TTL, the lock expired, a second
  worker acquired it, and the first worker then deleted the *second* worker's
  lock on its way out. A third job could start while the second was still
  running, which is exactly what the middleware exists to prevent.
- The release ran on the job's context. If that context was cancelled while
  the job ran (worker shutdown), the `DEL` failed and the lock stayed until
  its TTL, or forever with a TTL of 0.
- `RateLimited` and `ThrottlesExceptions` set the window with
  `EXPIRE key int(decay.Seconds())`. A window below one second became
  `EXPIRE key 0`, which deletes the counter immediately: no limiting at all.

## What changed

- Locks store a random owner token and are released by a Lua script that
  deletes the key only if the token still matches.
- The release uses `context.WithoutCancel`.
- Windows use `PEXPIRE` with millisecond precision (minimum 1ms), and a
  counter that has lost its TTL gets one again.

## What you need to do

- Nothing in code.
- **Rolling deploys:** old and new workers interoperate. An old worker still
  deletes unconditionally, so the ownership guarantee only holds once every
  worker runs the new version.
- **If you inspect lock keys yourself:** the value under
  `laravel_unique_job:*` and `laravel_overlapping:*` is now a random token
  instead of `1`.

These keys are this package's own. They are not the keys Laravel's
`ShouldBeUnique` / `WithoutOverlapping` use (those live in the application
cache store under version-specific names), so Go and PHP jobs do not exclude
each other through this middleware.
