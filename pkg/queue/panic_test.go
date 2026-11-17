package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/events"
	"github.com/rjp2525/laravel-queue-go/pkg/failed"
	"github.com/rjp2525/laravel-queue-go/pkg/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// panicTestDriver hands out a fixed list of jobs and counts what happens to them.
type panicTestDriver struct {
	mockDriver
	mu       sync.Mutex
	jobs     []*RawJob
	released int
	deleted  int
}

func (d *panicTestDriver) Pop(context.Context, string) (*RawJob, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.jobs) == 0 {
		return nil, nil
	}
	job := d.jobs[0]
	d.jobs = d.jobs[1:]
	return job, nil
}

func (d *panicTestDriver) Release(context.Context, *RawJob, time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.released++
	return nil
}

func (d *panicTestDriver) Delete(context.Context, *RawJob) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted++
	return nil
}

type failedRecords struct {
	mu      sync.Mutex
	records []failed.Record
}

func (r *failedRecords) Log(_ context.Context, rec failed.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func panicTestJob(name, extra string) *RawJob {
	return &RawJob{
		Body:  fmt.Sprintf(`{"uuid":"u","displayName":%q,"job":"j","data":{"commandName":%q,"command":""},"attempts":1%s}`, name, name, extra),
		Queue: "default",
	}
}

func TestWorkerRecoversHandlerPanic(t *testing.T) {
	for _, concurrency := range []int{1, 4} {
		t.Run(fmt.Sprintf("concurrency %d", concurrency), func(t *testing.T) {
			driver := &panicTestDriver{jobs: []*RawJob{
				panicTestJob("Boom", `,"maxTries":1`),
				panicTestJob("Fine", ``),
			}}
			failedLog := &failedRecords{}

			worker := NewWorker(driver, WorkerOptions{StopOnEmpty: true, Concurrency: concurrency})
			worker.SetFailedProvider(failedLog)
			worker.Register("Boom", func(context.Context, *Job) error {
				panic("handler exploded")
			})
			var fineRan bool
			var mu sync.Mutex
			worker.Register("Fine", func(context.Context, *Job) error {
				mu.Lock()
				defer mu.Unlock()
				fineRan = true
				return nil
			})
			var failure error
			worker.On(events.JobFailed, func(e events.Event) {
				mu.Lock()
				defer mu.Unlock()
				failure = e.Error
			})

			require.NoError(t, worker.Run(context.Background()))

			mu.Lock()
			defer mu.Unlock()
			assert.True(t, fineRan, "the worker keeps processing after a panic")

			var panicErr *PanicError
			require.ErrorAs(t, failure, &panicErr)
			assert.Equal(t, "panic in job handler: handler exploded", panicErr.Error())

			require.Len(t, failedLog.records, 1)
			assert.Contains(t, failedLog.records[0].Exception, "panic in job handler")
			assert.Contains(t, failedLog.records[0].Exception, "panic_test.go", "the stored exception includes the stack")
		})
	}
}

func TestWorkerRecoversMiddlewarePanicAndRetries(t *testing.T) {
	driver := &panicTestDriver{jobs: []*RawJob{panicTestJob("Job", `,"maxTries":3`)}}

	worker := NewWorker(driver, WorkerOptions{StopOnEmpty: true})
	worker.Use(func(context.Context, middleware.Job, func() error) error { panic("middleware exploded") })
	worker.Register("Job", func(context.Context, *Job) error { return nil })

	require.NoError(t, worker.Run(context.Background()))
	assert.Equal(t, 1, driver.released, "a panicking attempt is retried like any failed attempt")
	assert.Zero(t, driver.deleted)
}

func TestPanicErrorUnwrapsErrorValues(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := error(&PanicError{Value: sentinel})
	assert.ErrorIs(t, err, sentinel)
	assert.NoError(t, (&PanicError{Value: "text"}).Unwrap())
	assert.Equal(t, "panic in job handler: sentinel", fmt.Sprintf("%v", err))
}
