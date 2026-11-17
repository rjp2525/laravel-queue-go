package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/failed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ctxAwareDriver behaves like a real Redis client: every call fails once its
// context is cancelled.
type ctxAwareDriver struct {
	mockDriver
	mu       sync.Mutex
	jobs     []*RawJob
	deleted  int
	released int
}

func (d *ctxAwareDriver) Pop(ctx context.Context, _ string) (*RawJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.jobs) == 0 {
		return nil, nil
	}
	job := d.jobs[0]
	d.jobs = d.jobs[1:]
	return job, nil
}

func (d *ctxAwareDriver) Delete(ctx context.Context, _ *RawJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted++
	return nil
}

func (d *ctxAwareDriver) Release(ctx context.Context, _ *RawJob, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.released++
	return nil
}

type ctxAwareFailedLog struct{ logged int }

func (l *ctxAwareFailedLog) Log(ctx context.Context, _ failed.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.logged++
	return nil
}

func ackTestJob(extra string) *RawJob {
	return &RawJob{
		Body:  `{"uuid":"u","displayName":"Job","job":"j","data":{"commandName":"Job","command":""},"attempts":1` + extra + `}`,
		Queue: "default",
	}
}

func TestJobFinishedDuringShutdownIsStillAcknowledged(t *testing.T) {
	driver := &ctxAwareDriver{jobs: []*RawJob{ackTestJob(``)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker := NewWorker(driver, WorkerOptions{})
	worker.Register("Job", func(context.Context, *Job) error {
		cancel() // shutdown arrives while the job is running; the job completes
		return nil
	})

	_ = worker.Run(ctx)
	assert.Equal(t, 1, driver.deleted, "a completed job must be removed from the reserved set")
}

func TestJobFailingDuringShutdownIsStillReleased(t *testing.T) {
	driver := &ctxAwareDriver{jobs: []*RawJob{ackTestJob(`,"maxTries":5`)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker := NewWorker(driver, WorkerOptions{})
	worker.Register("Job", func(ctx context.Context, _ *Job) error {
		cancel()
		<-ctx.Done()
		return ctx.Err() // the handler gave up because of the shutdown
	})

	_ = worker.Run(ctx)
	assert.Equal(t, 1, driver.released, "the job goes back to the queue right away, not after retry_after")
}

func TestJobFailingForGoodDuringShutdownIsStillRecorded(t *testing.T) {
	driver := &ctxAwareDriver{jobs: []*RawJob{ackTestJob(`,"maxTries":1`)}}
	failedLog := &ctxAwareFailedLog{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker := NewWorker(driver, WorkerOptions{})
	worker.SetFailedProvider(failedLog)
	worker.Register("Job", func(context.Context, *Job) error {
		cancel()
		return errors.New("boom")
	})

	_ = worker.Run(ctx)
	assert.Equal(t, 1, driver.deleted)
	assert.Equal(t, 1, failedLog.logged, "the failure must reach failed_jobs")
}

func TestManualJobLifecycleIgnoresCancellation(t *testing.T) {
	driver := &ctxAwareDriver{}
	ctx, cancel := context.WithCancel(context.Background())

	job, err := ParseJob(ctx, ackTestJob(``), driver)
	require.NoError(t, err)
	cancel()

	require.NoError(t, job.Delete())
	require.NoError(t, job.Release(0))
}
