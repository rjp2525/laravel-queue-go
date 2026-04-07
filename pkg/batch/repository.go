package batch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rjp2525/laravel-queue-go/pkg/internal/dbutil"
)

type Repository struct {
	db    *sql.DB
	table string
}

func NewRepository(db *sql.DB, table string) (*Repository, error) {
	if table == "" {
		table = "job_batches"
	}
	if err := dbutil.ValidateTableName(table); err != nil {
		return nil, fmt.Errorf("batch: %w", err)
	}
	return &Repository{db: db, table: table}, nil
}

func (r *Repository) Find(ctx context.Context, id string) (*Batch, error) {
	query := `SELECT id, name, total_jobs, pending_jobs, failed_jobs, failed_job_ids, options, cancelled_at, created_at, finished_at FROM ` + r.table + ` WHERE id = ?`
	row := r.db.QueryRowContext(ctx, query, id)

	var b Batch
	var failedJobIDs string
	var cancelledAt, finishedAt sql.NullTime

	if err := row.Scan(&b.ID, &b.Name, &b.TotalJobs, &b.PendingJobs, &b.FailedJobs, &failedJobIDs, &b.Options, &cancelledAt, &b.CreatedAt, &finishedAt); err != nil {
		return nil, fmt.Errorf("batch: find %s: %w", id, err)
	}

	if failedJobIDs != "" {
		_ = json.Unmarshal([]byte(failedJobIDs), &b.FailedJobIDs)
	}
	if cancelledAt.Valid {
		b.CancelledAt = &cancelledAt.Time
	}
	if finishedAt.Valid {
		b.FinishedAt = &finishedAt.Time
	}

	return &b, nil
}

func (r *Repository) IsCancelled(ctx context.Context, id string) (bool, error) {
	query := `SELECT cancelled_at FROM ` + r.table + ` WHERE id = ?`
	var cancelledAt sql.NullTime
	if err := r.db.QueryRowContext(ctx, query, id).Scan(&cancelledAt); err != nil {
		return false, fmt.Errorf("batch: is_cancelled %s: %w", id, err)
	}
	return cancelledAt.Valid, nil
}

type CreateOptions struct {
	Name          string
	TotalJobs     int
	AllowFailures bool
	OnFinish      string
	OnSuccess     string
	OnError       string
}

func (r *Repository) Create(ctx context.Context, opts CreateOptions) (*Batch, error) {
	id := uuid.NewString()

	options, _ := json.Marshal(map[string]any{
		"allowFailures": opts.AllowFailures,
		"onFinish":      opts.OnFinish,
		"onSuccess":     opts.OnSuccess,
		"onError":       opts.OnError,
	})

	query := `INSERT INTO ` + r.table + ` (id, name, total_jobs, pending_jobs, failed_jobs, failed_job_ids, options, cancelled_at, created_at, finished_at) VALUES (?, ?, ?, ?, 0, '[]', ?, NULL, ?, NULL)`
	now := time.Now()
	_, err := r.db.ExecContext(ctx, query, id, opts.Name, opts.TotalJobs, opts.TotalJobs, string(options), now)
	if err != nil {
		return nil, fmt.Errorf("batch: create: %w", err)
	}

	return &Batch{
		ID:          id,
		Name:        opts.Name,
		TotalJobs:   opts.TotalJobs,
		PendingJobs: opts.TotalJobs,
		Options:     string(options),
		CreatedAt:   now,
	}, nil
}

// DecrementPendingJobs atomically decrements pending and marks finished if zero.
func (r *Repository) DecrementPendingJobs(ctx context.Context, id string) (*Batch, error) {
	// Atomic: decrement and mark finished in one statement to avoid TOCTOU.
	now := time.Now()
	query := `UPDATE ` + r.table + ` SET pending_jobs = pending_jobs - 1, finished_at = CASE WHEN pending_jobs = 1 THEN ? ELSE finished_at END WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, now, id); err != nil {
		return nil, fmt.Errorf("batch: decrement_pending %s: %w", id, err)
	}

	return r.Find(ctx, id)
}

// IncrementFailedJobs atomically increments the failure counter.
// Note: failed_job_ids append is best-effort (not atomic with concurrent writes).
func (r *Repository) IncrementFailedJobs(ctx context.Context, id string, jobID string) error {
	query := `UPDATE ` + r.table + ` SET failed_jobs = failed_jobs + 1, failed_job_ids = ? WHERE id = ?`

	// Read-then-write for the JSON array. Accept the race trade-off for portability.
	b, err := r.Find(ctx, id)
	if err != nil {
		return fmt.Errorf("batch: increment_failed %s: %w", id, err)
	}
	allFailed := append(b.FailedJobIDs, jobID)
	failedJSON, _ := json.Marshal(allFailed)

	_, err = r.db.ExecContext(ctx, query, string(failedJSON), id)
	if err != nil {
		return fmt.Errorf("batch: increment_failed %s: %w", id, err)
	}
	return nil
}

func (r *Repository) Cancel(ctx context.Context, id string) error {
	query := `UPDATE ` + r.table + ` SET cancelled_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("batch: cancel %s: %w", id, err)
	}
	return nil
}

func (r *Repository) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	query := `DELETE FROM ` + r.table + ` WHERE finished_at IS NOT NULL AND finished_at < ?`
	result, err := r.db.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("batch: prune: %w", err)
	}
	return result.RowsAffected()
}
