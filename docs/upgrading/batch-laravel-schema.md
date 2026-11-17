# batch: Repository now matches Laravel's job_batches table

**Breaking:** yes, if you created `job_batches` with a non-Laravel schema, or
read `Batch.Options` as JSON. Builds on
[failed-jobs-postgres](failed-jobs-postgres.md).

## What was wrong

`batch.Repository` did not work against the table Laravel's
`make:queue-batches-table` migration creates (identical in Laravel 10-13):

| | Laravel | Before |
|---|---|---|
| `created_at`, `cancelled_at`, `finished_at` | `integer` unix timestamps | written and scanned as `time.Time`: `Create` failed with `Data truncated for column 'created_at'`, `Find` could not scan |
| `options` | PHP `serialize()`, base64-encoded on PostgreSQL | written as JSON, which Laravel cannot unserialize |
| placeholders | per database | `?` only: every call failed on PostgreSQL |
| `cancel` | sets `cancelled_at` and `finished_at` | set only `cancelled_at`, so cancelled batches were never pruned |
| failed job ids | updated inside a transaction with a row lock, unique | read-then-write without a lock: concurrent failures overwrote each other |
| `id` | `Str::orderedUuid()` | random UUIDv4 |

## What changed

- Timestamps are read and written as unix integers. `Batch.CreatedAt`,
  `CancelledAt` and `FinishedAt` are still `time.Time` / `*time.Time`.
- `Create` writes options with PHP serialization (`allowFailures` under the
  key Laravel reads) and base64-encodes them on PostgreSQL. `Find` decodes
  them. New `Batch.AllowsFailures()`.
- SQL dialect detection and `batch.WithDialect`, as in the failed job
  provider.
- `Cancel` also sets `finished_at`.
- `IncrementFailedJobs` runs in a transaction with `SELECT ... FOR UPDATE`
  (MySQL, PostgreSQL) and lists each job id once.
- New batch ids are UUIDv7 (time-ordered).

## What you need to do

1. **If your table was created by Laravel:** nothing. The repository now
   works where it previously failed.
2. **If you created `job_batches` yourself with `DATETIME`/`TIMESTAMP`
   columns** to make the old code work, convert the three columns to integers
   to match Laravel. MySQL:

   ```sql
   ALTER TABLE job_batches
     ADD COLUMN created_at_i   INT NOT NULL DEFAULT 0,
     ADD COLUMN cancelled_at_i INT NULL,
     ADD COLUMN finished_at_i  INT NULL;
   UPDATE job_batches SET
     created_at_i   = UNIX_TIMESTAMP(created_at),
     cancelled_at_i = UNIX_TIMESTAMP(cancelled_at),
     finished_at_i  = UNIX_TIMESTAMP(finished_at);
   ALTER TABLE job_batches
     DROP COLUMN created_at, DROP COLUMN cancelled_at, DROP COLUMN finished_at,
     RENAME COLUMN created_at_i TO created_at,
     RENAME COLUMN cancelled_at_i TO cancelled_at,
     RENAME COLUMN finished_at_i TO finished_at;
   ```

   PostgreSQL: the same with `EXTRACT(EPOCH FROM created_at)::int`.
3. **If you parsed `Batch.Options` as JSON:** it is now PHP `serialize()`
   output. Use `b.AllowsFailures()`, or decode it:

   ```go
   decoded, _ := phpserialize.Decode(b.Options)
   opts := decoded.(*phpserialize.Array).ToMap() // "allowFailures", "onFinish", ...
   ```

   Rows written by the previous version hold JSON in `options`. Laravel
   treats unreadable options as empty, and `AllowsFailures()` returns false
   for them. Re-create or update those batches if the flag matters.
4. **`Cancel` now marks the batch finished** (`Finished()` is true for a
   cancelled batch, and `Prune` removes it). Check `Cancelled()` before
   `Finished()` if you distinguish the two.
5. **Wrapped SQL drivers:** pass `batch.WithDialect(dialect.Postgres)` (or
   the right one), as described in the failed job provider guide.
