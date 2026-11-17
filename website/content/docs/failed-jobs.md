---
title: Failed jobs
description: Store failures in Laravel's failed_jobs table so queue:failed and queue:retry work on jobs a Go worker gave up on.
---

When a job runs out of attempts it's removed from the queue. Where it goes next is up to the failed job provider. Out of the box that's nowhere: the default provider discards failures, so **configure one before you go to production**.

## Using Laravel's table

`DatabaseProvider` writes to the `failed_jobs` table your Laravel app already has, in the format of Laravel's default `database-uuids` driver.

```go
import (
    "database/sql"

    _ "github.com/go-sql-driver/mysql"
    "github.com/rjp2525/laravel-queue-go/pkg/failed"
)

db, err := sql.Open("mysql", dsn)
if err != nil {
    log.Fatal(err)
}

provider, err := failed.NewDatabaseProvider(db, "failed_jobs")
if err != nil {
    log.Fatal(err)
}
mgr.SetFailedProvider(provider)
```

From then on, a job that fails in Go shows up exactly where a PHP failure would:

```bash
php artisan queue:failed
php artisan queue:retry 0b1e7c6a-...   # puts it back on the queue
php artisan queue:retry all
```

A retried job goes back to its original queue, where your Go worker picks it up again.

## Databases

MySQL, MariaDB, PostgreSQL, SQLite and SQL Server all work. The provider looks at the driver behind your `*sql.DB` and writes the right kind of placeholders.

If your `*sql.DB` is wrapped by an instrumentation library (OpenTelemetry, Datadog and the like), the real driver is hidden. Say what's underneath:

```go
import "github.com/rjp2525/laravel-queue-go/pkg/dialect"

provider, err := failed.NewDatabaseProvider(db, "failed_jobs",
    failed.WithDialect(dialect.Postgres))
```

## What gets stored

| Column | Value |
|---|---|
| `uuid` | The job's UUID |
| `connection` | The connection name, `redis` unless you called `mgr.SetConnection` |
| `queue` | The queue the job was on |
| `payload` | The job's payload as Laravel pushed it |
| `exception` | The error, with a stack trace if the handler panicked |
| `failed_at` | When it failed |

Jobs with no registered handler and jobs whose payload couldn't be parsed are stored too, so nothing disappears quietly.

## Reading and cleaning up from Go

`DatabaseProvider` can also query the table:

```go
jobs, err := provider.All(ctx)     // newest first
job, err := provider.Find(ctx, id)
n, err := provider.Count(ctx)

err = provider.Forget(ctx, id)     // delete one
err = provider.Flush(ctx)          // delete all
```

`All` loads every row. On a table with a long history, prune it first (`php artisan queue:prune-failed`) or query it directly.

## Your own storage

The worker only needs one method. Implement it to send failures wherever you like: a different table, an error tracker, a dead-letter queue.

```go
type Logger interface {
    Log(ctx context.Context, record failed.Record) error
}
```

```go
type sentryFailures struct{}

func (sentryFailures) Log(ctx context.Context, r failed.Record) error {
    sentry.CaptureMessage(fmt.Sprintf("job %s failed on %s: %s", r.UUID, r.Queue, r.Exception))
    return nil
}

mgr.SetFailedProvider(sentryFailures{})
```

If `Log` returns an error the worker logs it and moves on. The job has already been taken off the queue at that point, so a provider that can fail (a database that might be down) is worth pairing with a `JobFailed` [event listener](/docs/events/) as a second line of defence.
