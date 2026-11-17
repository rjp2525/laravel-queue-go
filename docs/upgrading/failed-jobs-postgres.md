# failed: PostgreSQL and SQL Server support

**Breaking:** no. `NewDatabaseProvider` gained optional trailing arguments.

## What was wrong

- Every query used `?` placeholders. PostgreSQL (`lib/pq`, `pgx`) and SQL
  Server reject them, so `Log`, `Find` and `Forget` failed with a syntax
  error and no failed job was ever recorded on those databases.
- The exception text was inserted as-is. An error message quoting bytes that
  are not valid UTF-8 made the insert fail on MySQL (`Incorrect string value`)
  and PostgreSQL, losing the failed job record.

## What changed

- New package `pkg/dialect`. The provider detects the database from the
  `*sql.DB`'s driver and rewrites placeholders (`$1` for PostgreSQL, `@p1`
  for SQL Server).
- `failed.WithDialect(d)` overrides detection.
- The exception is made valid UTF-8 before insert, as Laravel does with
  `mb_convert_encoding`.

## What you need to do

Nothing for MySQL, SQLite, `lib/pq`, `pgx` and `go-mssqldb`.

If your `*sql.DB` uses a **wrapped driver** (OpenTelemetry `otelsql`,
`sqlmw`, Datadog `sqltrace`, ...), detection sees the wrapper and falls back
to `?`. Tell the provider what is underneath:

```go
import "github.com/rjp2525/laravel-queue-go/pkg/dialect"

provider, err := failed.NewDatabaseProvider(db, "failed_jobs",
    failed.WithDialect(dialect.Postgres))
```

If you worked around the old behaviour with a driver-level placeholder
rewriter, remove it or pass `failed.WithDialect(dialect.MySQL)` to keep `?`.

The table layout is unchanged: Laravel's `failed_jobs` (`uuid`, `connection`,
`queue`, `payload`, `exception`, `failed_at`), identical from Laravel 10 to 13.
