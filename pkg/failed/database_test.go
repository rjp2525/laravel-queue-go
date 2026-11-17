package failed

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/dialect"
	"github.com/rjp2525/laravel-queue-go/pkg/internal/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogUsesDialectPlaceholders(t *testing.T) {
	for d, want := range map[dialect.Dialect]string{
		dialect.MySQL:     `INSERT INTO failed_jobs (uuid, connection, queue, payload, exception, failed_at) VALUES (?, ?, ?, ?, ?, ?)`,
		dialect.SQLite:    `INSERT INTO failed_jobs (uuid, connection, queue, payload, exception, failed_at) VALUES (?, ?, ?, ?, ?, ?)`,
		dialect.Postgres:  `INSERT INTO failed_jobs (uuid, connection, queue, payload, exception, failed_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		dialect.SQLServer: `INSERT INTO failed_jobs (uuid, connection, queue, payload, exception, failed_at) VALUES (@p1, @p2, @p3, @p4, @p5, @p6)`,
	} {
		t.Run(d.String(), func(t *testing.T) {
			db, rec := dbtest.Open()
			p, err := NewDatabaseProvider(db, "", WithDialect(d))
			require.NoError(t, err)

			require.NoError(t, p.Log(context.Background(), Record{
				UUID:       "u-1",
				Connection: "redis",
				Queue:      "default",
				Payload:    []byte(`{"uuid":"u-1"}`),
				Exception:  "boom",
			}))

			call := rec.Last()
			assert.Equal(t, want, call.Query)
			require.Len(t, call.Args, 6)
			assert.Equal(t, []any{"u-1", "redis", "default", `{"uuid":"u-1"}`, "boom"}, call.Args[:5])
			assert.IsType(t, time.Time{}, call.Args[5])
		})
	}
}

func TestFindAndForgetUseDialectPlaceholders(t *testing.T) {
	db, rec := dbtest.Open()
	p, err := NewDatabaseProvider(db, "failed_jobs", WithDialect(dialect.Postgres))
	require.NoError(t, err)

	failedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rec.QueueRows(
		[]string{"id", "uuid", "connection", "queue", "payload", "exception", "failed_at"},
		[]driver.Value{int64(7), "u-7", "redis", "default", "{}", "boom", failedAt},
	)

	job, err := p.Find(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, "u-7", job.UUID)
	assert.Equal(t, failedAt, job.FailedAt)
	assert.Contains(t, rec.Last().Query, "WHERE id = $1")

	require.NoError(t, p.Forget(context.Background(), 7))
	assert.Equal(t, `DELETE FROM failed_jobs WHERE id = $1`, rec.Last().Query)
}

func TestDialectDefaultsToDriverDetection(t *testing.T) {
	// The recording driver is not a known one, so "?" placeholders are kept.
	db, rec := dbtest.Open()
	p, err := NewDatabaseProvider(db, "")
	require.NoError(t, err)

	require.NoError(t, p.Forget(context.Background(), 1))
	assert.Equal(t, `DELETE FROM failed_jobs WHERE id = ?`, rec.Last().Query)
}

func TestLogGeneratesUUIDAndSanitisesException(t *testing.T) {
	db, rec := dbtest.Open()
	p, err := NewDatabaseProvider(db, "")
	require.NoError(t, err)

	// PostgreSQL rejects text that is not valid UTF-8; an error message
	// quoting binary job data must not make the insert itself fail.
	require.NoError(t, p.Log(context.Background(), Record{Exception: "bad byte: \xff end"}))

	args := rec.Last().Args
	assert.Len(t, args[0], 36, "a UUID is generated when the record has none")
	assert.Equal(t, "bad byte: � end", args[4])
}

func TestNewDatabaseProviderRejectsUnsafeTableName(t *testing.T) {
	db, _ := dbtest.Open()
	_, err := NewDatabaseProvider(db, "failed_jobs; DROP TABLE users")
	assert.Error(t, err)
}
