package failed

import (
	"context"
	"time"
)

type FailedJob struct {
	ID         int64
	UUID       string
	Connection string
	Queue      string
	Payload    string
	Exception  string
	FailedAt   time.Time
}

type Record struct {
	UUID       string // pre-extracted from the job; avoids re-parsing the payload
	Connection string
	Queue      string
	Payload    []byte
	Exception  string
}

// Logger is the minimal interface needed to record a failed job.
type Logger interface {
	Log(ctx context.Context, record Record) error
}

// Store extends Logger with query and management operations.
type Store interface {
	Logger
	Find(ctx context.Context, id int64) (*FailedJob, error)
	All(ctx context.Context) ([]*FailedJob, error)
	Forget(ctx context.Context, id int64) error
	Flush(ctx context.Context) error
	Count(ctx context.Context) (int64, error)
}

// Provider is an alias kept for backward compatibility; prefer Logger or Store.
type Provider = Store
