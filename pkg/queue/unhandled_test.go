package queue

import (
	"context"
	"sync"
	"testing"

	"github.com/rjp2525/laravel-queue-go/v2/pkg/events"
	"github.com/rjp2525/laravel-queue-go/v2/pkg/failed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type unhandledFailedLog struct {
	mu      sync.Mutex
	records []failed.Record
}

func (l *unhandledFailedLog) Log(_ context.Context, rec failed.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rec)
	return nil
}

func runUnhandled(t *testing.T, raw *RawJob, configure func(*Worker)) (*ctxAwareDriver, *unhandledFailedLog, []events.Event) {
	t.Helper()
	driver := &ctxAwareDriver{jobs: []*RawJob{raw}}
	failedLog := &unhandledFailedLog{}
	var fired []events.Event

	worker := NewWorker(driver, WorkerOptions{StopOnEmpty: true})
	worker.SetFailedProvider(failedLog)
	worker.On(events.JobFailed, func(e events.Event) { fired = append(fired, e) })
	if configure != nil {
		configure(worker)
	}

	require.NoError(t, worker.Run(context.Background()))
	return driver, failedLog, fired
}

func TestJobWithoutHandlerIsFailedNotDropped(t *testing.T) {
	const body = `{"uuid":"u-42","displayName":"App\\Jobs\\OnlyInPHP","job":"j","data":{"commandName":"App\\Jobs\\OnlyInPHP","command":""},"attempts":1}`
	driver, failedLog, fired := runUnhandled(t, &RawJob{Body: body, Queue: "default"}, nil)

	assert.Equal(t, 1, driver.deleted)
	assert.Zero(t, driver.released, "releasing would only bounce the job between workers")

	require.Len(t, failedLog.records, 1, "the job must be recoverable from failed_jobs")
	rec := failedLog.records[0]
	assert.Equal(t, "u-42", rec.UUID)
	assert.Equal(t, "default", rec.Queue)
	assert.JSONEq(t, body, string(rec.Payload))
	assert.Contains(t, rec.Exception, `no handler registered for App\Jobs\OnlyInPHP`)

	require.Len(t, fired, 1)
	assert.ErrorIs(t, fired[0].Error, ErrNoHandler)
	assert.Equal(t, `App\Jobs\OnlyInPHP`, fired[0].JobName)
}

func TestDefaultHandlerStillReceivesUnregisteredJobs(t *testing.T) {
	const body = `{"uuid":"u","displayName":"Anything","job":"j","data":{"commandName":"Anything","command":""},"attempts":1}`
	var got string
	driver, failedLog, fired := runUnhandled(t, &RawJob{Body: body, Queue: "default"}, func(w *Worker) {
		w.RegisterDefault(func(_ context.Context, job *Job) error {
			got = job.DisplayName()
			return nil
		})
	})

	assert.Equal(t, "Anything", got)
	assert.Equal(t, 1, driver.deleted)
	assert.Empty(t, failedLog.records)
	assert.Empty(t, fired)
}

func TestUnparseablePayloadIsFailedNotDropped(t *testing.T) {
	const body = `{"uuid": this is not json`
	driver, failedLog, fired := runUnhandled(t, &RawJob{Body: body, Queue: "emails"}, nil)

	assert.Equal(t, 1, driver.deleted)
	require.Len(t, failedLog.records, 1)
	assert.Equal(t, body, string(failedLog.records[0].Payload), "the raw payload is kept for inspection")
	assert.Equal(t, "emails", failedLog.records[0].Queue)
	assert.Contains(t, failedLog.records[0].Exception, "parse job payload")

	require.Len(t, fired, 1)
	assert.Equal(t, "emails", fired[0].Queue)
	assert.Error(t, fired[0].Error)
}
