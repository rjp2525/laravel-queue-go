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

// RawJob is the raw data from the driver. Body is stored as string to avoid
// repeated []byte<->string copies between Redis (string) and JSON ([]byte).
type RawJob struct {
	Body          string
	Queue         string
	ReservedUntil time.Time
}
