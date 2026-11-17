package batch

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rjp2525/laravel-queue-go/pkg/dialect"
	"github.com/rjp2525/laravel-queue-go/pkg/internal/dbutil"
	"github.com/rjp2525/laravel-queue-go/pkg/phpserialize"
)

// Repository reads and writes Laravel's job_batches table, in the layout
// Illuminate\Bus\DatabaseBatchRepository uses (unchanged from Laravel 10 to
// 13): unix-timestamp integer columns, a JSON failed_job_ids array, and
// PHP-serialized options that are base64-encoded on PostgreSQL.
type Repository struct {
	db      *sql.DB
	table   string
	dialect dialect.Dialect
}

// Option configures a Repository.
type Option func(*Repository)

// WithDialect sets the SQL dialect instead of detecting it from the driver.
func WithDialect(d dialect.Dialect) Option {
	return func(r *Repository) { r.dialect = d }
}

func NewRepository(db *sql.DB, table string, opts ...Option) (*Repository, error) {
	if table == "" {
		table = "job_batches"
	}
	if err := dbutil.ValidateTableName(table); err != nil {
		return nil, fmt.Errorf("batch: %w", err)
	}
	r := &Repository{db: db, table: table}
	for _, opt := range opts {
		opt(r)
	}
	r.dialect = r.dialect.Resolve(db)
	return r, nil
}

// q adapts a query's "?" placeholders to the database in use.
func (r *Repository) q(query string) string {
	return r.dialect.Rebind(query)
}

const batchColumns = `id, name, total_jobs, pending_jobs, failed_jobs, failed_job_ids, options, cancelled_at, created_at, finished_at`

// queryer is the part of *sql.DB and *sql.Tx that find needs.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *Repository) Find(ctx context.Context, id string) (*Batch, error) {
	return r.find(ctx, r.db, id, "")
}

func (r *Repository) find(ctx context.Context, db queryer, id, lock string) (*Batch, error) {
	query := `SELECT ` + batchColumns + ` FROM ` + r.table + ` WHERE id = ?` + lock
	row := db.QueryRowContext(ctx, r.q(query), id)

	var (
		b                       Batch
		failedJobIDs            sql.NullString
		options                 sql.NullString
		cancelledAt, finishedAt sql.NullInt64
		createdAt               int64
	)
	if err := row.Scan(&b.ID, &b.Name, &b.TotalJobs, &b.PendingJobs, &b.FailedJobs, &failedJobIDs, &options, &cancelledAt, &createdAt, &finishedAt); err != nil {
		return nil, fmt.Errorf("batch: find %s: %w", id, err)
	}

	if failedJobIDs.String != "" {
		if err := json.Unmarshal([]byte(failedJobIDs.String), &b.FailedJobIDs); err != nil {
			return nil, fmt.Errorf("batch: find %s: decode failed_job_ids: %w", id, err)
		}
	}
	b.Options = r.decodeOptions(options.String)
	b.CreatedAt = time.Unix(createdAt, 0)
	if cancelledAt.Valid {
		b.CancelledAt = new(time.Unix(cancelledAt.Int64, 0))
	}
	if finishedAt.Valid {
		b.FinishedAt = new(time.Unix(finishedAt.Int64, 0))
	}

	return &b, nil
}

func (r *Repository) IsCancelled(ctx context.Context, id string) (bool, error) {
	query := `SELECT cancelled_at FROM ` + r.table + ` WHERE id = ?`
	var cancelledAt sql.NullInt64
	if err := r.db.QueryRowContext(ctx, r.q(query), id).Scan(&cancelledAt); err != nil {
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

// Create inserts a batch. Its options are stored PHP-serialized, as Laravel
// stores them, with "allowFailures" under the key Laravel reads. OnFinish,
// OnSuccess and OnError are kept as plain strings for Go consumers; Laravel's
// own then/catch/finally callbacks are serialized PHP closures, which cannot
// be produced here.
func (r *Repository) Create(ctx context.Context, opts CreateOptions) (*Batch, error) {
	// Laravel uses Str::orderedUuid(); UUIDv7 is the time-ordered equivalent.
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("batch: create: %w", err)
	}

	options, err := phpserialize.Encode(map[string]any{
		"allowFailures": opts.AllowFailures,
		"onFinish":      opts.OnFinish,
		"onSuccess":     opts.OnSuccess,
		"onError":       opts.OnError,
	})
	if err != nil {
		return nil, fmt.Errorf("batch: create: serialize options: %w", err)
	}

	query := `INSERT INTO ` + r.table + ` (` + batchColumns + `) VALUES (?, ?, ?, ?, 0, '[]', ?, NULL, ?, NULL)`
	now := time.Now()
	_, err = r.db.ExecContext(ctx, r.q(query), id.String(), opts.Name, opts.TotalJobs, opts.TotalJobs, r.encodeOptions(options), now.Unix())
	if err != nil {
		return nil, fmt.Errorf("batch: create: %w", err)
	}

	return &Batch{
		ID:          id.String(),
		Name:        opts.Name,
		TotalJobs:   opts.TotalJobs,
		PendingJobs: opts.TotalJobs,
		Options:     options,
		CreatedAt:   time.Unix(now.Unix(), 0),
	}, nil
}

// encodeOptions prepares serialized options for storage. Laravel
// base64-encodes them on PostgreSQL, whose text columns cannot hold the NUL
// bytes PHP serialization may contain.
func (r *Repository) encodeOptions(serialized string) string {
	if r.dialect == dialect.Postgres {
		return base64.StdEncoding.EncodeToString([]byte(serialized))
	}
	return serialized
}

// decodeOptions reverses encodeOptions with Laravel's own test: on
// PostgreSQL a value without ':' or ';' cannot be serialized PHP, so it is
// base64.
func (r *Repository) decodeOptions(stored string) string {
	if r.dialect != dialect.Postgres || strings.ContainsAny(stored, ":;") {
		return stored
	}
	decoded, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return stored
	}
	return string(decoded)
}

// DecrementPendingJobs atomically decrements pending and marks finished if zero.
func (r *Repository) DecrementPendingJobs(ctx context.Context, id string) (*Batch, error) {
	// Atomic: decrement and mark finished in one statement to avoid TOCTOU.
	query := `UPDATE ` + r.table + ` SET pending_jobs = pending_jobs - 1, finished_at = CASE WHEN pending_jobs = 1 THEN ? ELSE finished_at END WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, r.q(query), time.Now().Unix(), id); err != nil {
		return nil, fmt.Errorf("batch: decrement_pending %s: %w", id, err)
	}

	return r.Find(ctx, id)
}

// IncrementFailedJobs records a failed job: it increments failed_jobs and
// adds jobID to failed_job_ids. Like Laravel, it does this in a transaction
// that locks the batch row, so concurrent failures do not overwrite each
// other's ids, and a job id is only listed once.
func (r *Repository) IncrementFailedJobs(ctx context.Context, id string, jobID string) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("batch: increment_failed %s: %w", id, err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(fmt.Errorf("batch: increment_failed %s: %w", id, err), ignoreTxDone(tx.Rollback()))
		}
	}()

	b, err := r.find(ctx, tx, id, r.dialect.LockForUpdate())
	if err != nil {
		return err
	}

	ids := b.FailedJobIDs
	if !slices.Contains(ids, jobID) {
		ids = append(ids, jobID)
	}
	failedJSON, err := json.Marshal(ids)
	if err != nil {
		return err
	}

	query := `UPDATE ` + r.table + ` SET failed_jobs = failed_jobs + 1, failed_job_ids = ? WHERE id = ?`
	if _, err = tx.ExecContext(ctx, r.q(query), string(failedJSON), id); err != nil {
		return err
	}
	return tx.Commit()
}

func ignoreTxDone(err error) error {
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return err
}

// Cancel marks the batch cancelled. Like Laravel, it also sets finished_at,
// which is what makes a cancelled batch prunable.
func (r *Repository) Cancel(ctx context.Context, id string) error {
	now := time.Now().Unix()
	query := `UPDATE ` + r.table + ` SET cancelled_at = ?, finished_at = ? WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, r.q(query), now, now, id); err != nil {
		return fmt.Errorf("batch: cancel %s: %w", id, err)
	}
	return nil
}

func (r *Repository) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan).Unix()
	query := `DELETE FROM ` + r.table + ` WHERE finished_at IS NOT NULL AND finished_at < ?`
	result, err := r.db.ExecContext(ctx, r.q(query), cutoff)
	if err != nil {
		return 0, fmt.Errorf("batch: prune: %w", err)
	}
	return result.RowsAffected()
}
