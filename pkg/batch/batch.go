package batch

import "time"

type Batch struct {
	ID           string
	Name         string
	TotalJobs    int
	PendingJobs  int
	FailedJobs   int
	FailedJobIDs []string
	Options      string // JSON-encoded batch options
	CancelledAt  *time.Time
	CreatedAt    time.Time
	FinishedAt   *time.Time
}

func (b *Batch) Cancelled() bool   { return b.CancelledAt != nil }
func (b *Batch) Finished() bool    { return b.FinishedAt != nil }
func (b *Batch) HasFailures() bool { return b.FailedJobs > 0 }

func (b *Batch) Progress() int {
	if b.TotalJobs == 0 {
		return 0
	}
	return ((b.TotalJobs - b.PendingJobs) * 100) / b.TotalJobs
}
