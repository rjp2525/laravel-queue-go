package queue

import (
	"context"
	"time"
)

type Driver interface {
	Push(ctx context.Context, queue string, payload []byte) (string, error)
	Later(ctx context.Context, queue string, delay time.Duration, payload []byte) (string, error)
	Pop(ctx context.Context, queue string) (*RawJob, error)
	Delete(ctx context.Context, job *RawJob) error
	Release(ctx context.Context, job *RawJob, delay time.Duration) error
	Size(ctx context.Context, queue string) (int64, error)
	Clear(ctx context.Context, queue string) (int64, error)
	MigrateExpiredJobs(ctx context.Context, queue string) error
	Close() error
}

// RawJob is the raw data from the driver. Payloads are stored as strings to
// avoid repeated []byte<->string copies between Redis (string) and JSON ([]byte).
type RawJob struct {
	// Body is the payload exactly as it was pushed. It is what gets recorded
	// in failed_jobs, matching Laravel's Job::getRawBody().
	Body string
	// Reserved is the payload as the driver holds it while the job is being
	// processed, with "attempts" already incremented for this run. Delete and
	// Release must address the job by this exact string. Drivers that do not
	// keep a separate reserved copy may leave it empty; Body is used instead.
	Reserved      string
	Queue         string
	ReservedUntil time.Time
}

// reserved returns the string that identifies the job in the driver's
// reserved set.
func (r *RawJob) reserved() string {
	if r.Reserved != "" {
		return r.Reserved
	}
	return r.Body
}
