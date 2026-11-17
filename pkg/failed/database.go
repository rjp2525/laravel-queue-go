package failed

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rjp2525/laravel-queue-go/pkg/dialect"
	"github.com/rjp2525/laravel-queue-go/pkg/internal/dbutil"
)

// DatabaseProvider stores failed jobs in Laravel's failed_jobs table, in the
// layout of the "database-uuids" driver (Laravel's default since version 8).
type DatabaseProvider struct {
	db      *sql.DB
	table   string
	dialect dialect.Dialect
}

// DatabaseOption configures a DatabaseProvider.
type DatabaseOption func(*DatabaseProvider)

// WithDialect sets the SQL dialect instead of detecting it from the driver.
// Needed when the driver is wrapped (tracing, pooling) and detection cannot
// see the real one.
func WithDialect(d dialect.Dialect) DatabaseOption {
	return func(p *DatabaseProvider) { p.dialect = d }
}

// NewDatabaseProvider returns a provider for the given table ("failed_jobs"
// when empty). The SQL dialect is detected from db's driver, so MySQL,
// PostgreSQL, SQLite and SQL Server work without configuration.
func NewDatabaseProvider(db *sql.DB, table string, opts ...DatabaseOption) (*DatabaseProvider, error) {
	if table == "" {
		table = "failed_jobs"
	}
	if err := dbutil.ValidateTableName(table); err != nil {
		return nil, fmt.Errorf("failed: %w", err)
	}
	p := &DatabaseProvider{db: db, table: table}
	for _, opt := range opts {
		opt(p)
	}
	p.dialect = p.dialect.Resolve(db)
	return p, nil
}

// q adapts a query's "?" placeholders to the database in use.
func (p *DatabaseProvider) q(query string) string {
	return p.dialect.Rebind(query)
}

func (p *DatabaseProvider) Log(ctx context.Context, record Record) error {
	jobUUID := record.UUID
	if jobUUID == "" {
		jobUUID = uuid.NewString()
	}

	query := `INSERT INTO ` + p.table + ` (uuid, connection, queue, payload, exception, failed_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := p.db.ExecContext(ctx, p.q(query), jobUUID, record.Connection, record.Queue, string(record.Payload), strings.ToValidUTF8(record.Exception, "\uFFFD"), time.Now())
	if err != nil {
		return fmt.Errorf("failed_jobs: log: %w", err)
	}
	return nil
}

func (p *DatabaseProvider) Find(ctx context.Context, id int64) (*FailedJob, error) {
	query := `SELECT id, uuid, connection, queue, payload, exception, failed_at FROM ` + p.table + ` WHERE id = ?`
	row := p.db.QueryRowContext(ctx, p.q(query), id)

	var fj FailedJob
	if err := row.Scan(&fj.ID, &fj.UUID, &fj.Connection, &fj.Queue, &fj.Payload, &fj.Exception, &fj.FailedAt); err != nil {
		return nil, fmt.Errorf("failed_jobs: find %d: %w", id, err)
	}
	return &fj, nil
}

func (p *DatabaseProvider) All(ctx context.Context) ([]*FailedJob, error) {
	query := `SELECT id, uuid, connection, queue, payload, exception, failed_at FROM ` + p.table + ` ORDER BY id DESC`
	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed_jobs: all: %w", err)
	}
	defer rows.Close()

	var jobs []*FailedJob
	for rows.Next() {
		var fj FailedJob
		if err := rows.Scan(&fj.ID, &fj.UUID, &fj.Connection, &fj.Queue, &fj.Payload, &fj.Exception, &fj.FailedAt); err != nil {
			return nil, fmt.Errorf("failed_jobs: scan: %w", err)
		}
		jobs = append(jobs, &fj)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed_jobs: all: %w", err)
	}
	return jobs, nil
}

func (p *DatabaseProvider) Forget(ctx context.Context, id int64) error {
	query := `DELETE FROM ` + p.table + ` WHERE id = ?`
	_, err := p.db.ExecContext(ctx, p.q(query), id)
	if err != nil {
		return fmt.Errorf("failed_jobs: forget %d: %w", id, err)
	}
	return nil
}

func (p *DatabaseProvider) Flush(ctx context.Context) error {
	query := `DELETE FROM ` + p.table
	_, err := p.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed_jobs: flush: %w", err)
	}
	return nil
}

func (p *DatabaseProvider) Count(ctx context.Context) (int64, error) {
	query := `SELECT COUNT(*) FROM ` + p.table
	var count int64
	if err := p.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed_jobs: count: %w", err)
	}
	return count, nil
}

type NullProvider struct{}

func (NullProvider) Log(_ context.Context, _ Record) error               { return nil }
func (NullProvider) Find(_ context.Context, _ int64) (*FailedJob, error) { return nil, sql.ErrNoRows }
func (NullProvider) All(_ context.Context) ([]*FailedJob, error)         { return nil, nil }
func (NullProvider) Forget(_ context.Context, _ int64) error             { return nil }
func (NullProvider) Flush(_ context.Context) error                       { return nil }
func (NullProvider) Count(_ context.Context) (int64, error)              { return 0, nil }
