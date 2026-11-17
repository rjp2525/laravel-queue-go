package batch

import (
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/phpserialize"
)

type Batch struct {
	ID           string
	Name         string
	TotalJobs    int
	PendingJobs  int
	FailedJobs   int
	FailedJobIDs []string
	// Options is the batch's options as PHP serialize() output (already
	// base64-decoded when read from PostgreSQL). See AllowsFailures.
	Options     string
	CancelledAt *time.Time
	CreatedAt   time.Time
	FinishedAt  *time.Time
}

func (b *Batch) Cancelled() bool   { return b.CancelledAt != nil }
func (b *Batch) Finished() bool    { return b.FinishedAt != nil }
func (b *Batch) HasFailures() bool { return b.FailedJobs > 0 }

// AllowsFailures reports whether the batch keeps running after a job fails
// (Laravel's allowFailures option). It is false when the options cannot be
// read.
func (b *Batch) AllowsFailures() bool {
	decoded, err := phpserialize.Decode(b.Options)
	if err != nil {
		return false
	}
	options, ok := decoded.(*phpserialize.Array)
	if !ok {
		return false
	}
	allow, _ := options.ToMap()["allowFailures"].(bool)
	return allow
}

func (b *Batch) Progress() int {
	if b.TotalJobs == 0 {
		return 0
	}
	return ((b.TotalJobs - b.PendingJobs) * 100) / b.TotalJobs
}
