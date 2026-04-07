package failed

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rjp2525/laravel-queue-go/pkg/internal/dbutil"
)

type DatabaseProvider struct {
	db    *sql.DB
	table string
}

func NewDatabaseProvider(db *sql.DB, table string) (*DatabaseProvider, error) {
	if table == "" {
		table = "failed_jobs"
	}
	if err := dbutil.ValidateTableName(table); err != nil {
		return nil, fmt.Errorf("failed: %w", err)
	}
	return &DatabaseProvider{db: db, table: table}, nil
}

func (p *DatabaseProvider) Log(ctx context.Context, record Record) error {
	jobUUID := record.UUID
	if jobUUID == "" {
		jobUUID = uuid.NewString()
	}

	query := `INSERT INTO ` + p.table + ` (uuid, connection, queue, payload, exception, failed_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := p.db.ExecContext(ctx, query, jobUUID, record.Connection, record.Queue, string(record.Payload), record.Exception, time.Now())
	if err != nil {
		return fmt.Errorf("failed_jobs: log: %w", err)
	}
	return nil
}

func (p *DatabaseProvider) Find(ctx context.Context, id int64) (*FailedJob, error) {
	query := `SELECT id, uuid, connection, queue, payload, exception, failed_at FROM ` + p.table + ` WHERE id = ?`
	row := p.db.QueryRowContext(ctx, query, id)

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
	_, err := p.db.ExecContext(ctx, query, id)
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
