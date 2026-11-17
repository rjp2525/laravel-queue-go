package queue

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests run in a synctest bubble, so the payload timeouts (whole
// seconds) elapse on a fake clock.

// waitForCancel is a well-behaved handler: it works until its context ends.
func waitForCancel(ctx context.Context, _ *Job) error {
	<-ctx.Done()
	return ctx.Err()
}

func runTimeoutJob(t *testing.T, opts WorkerOptions, payloadExtra string, handler Handler) (driver *panicTestDriver, failure, exception error, elapsed time.Duration) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		driver = &panicTestDriver{jobs: []*RawJob{panicTestJob("Slow", payloadExtra)}}
		opts.StopOnEmpty = true
		worker := NewWorker(driver, opts)
		worker.Register("Slow", handler)
		worker.On(events.JobFailed, func(e events.Event) { failure = e.Error })
		worker.On(events.JobExceptionOccurred, func(e events.Event) { exception = e.Error })

		start := time.Now()
		require.NoError(t, worker.Run(context.Background()))
		elapsed = time.Since(start)
	})
	return driver, failure, exception, elapsed
}

func TestWorkerEnforcesPayloadTimeout(t *testing.T) {
	driver, failure, exception, elapsed := runTimeoutJob(t, WorkerOptions{}, `,"timeout":2,"maxTries":5`, waitForCancel)

	assert.Equal(t, 2*time.Second, elapsed, "the handler's context is cancelled at the payload timeout")

	var timeoutErr *TimeoutError
	require.ErrorAs(t, exception, &timeoutErr)
	assert.Equal(t, 2*time.Second, timeoutErr.Timeout)
	assert.ErrorIs(t, exception, context.DeadlineExceeded, "the handler's own error is kept")

	assert.NoError(t, failure, "a timed-out attempt is retried while tries remain")
	assert.Equal(t, 1, driver.released)
}

func TestWorkerFailOnTimeoutFailsWithoutRetry(t *testing.T) {
	driver, failure, _, _ := runTimeoutJob(t, WorkerOptions{}, `,"timeout":1,"failOnTimeout":true,"maxTries":5`, waitForCancel)

	var timeoutErr *TimeoutError
	require.ErrorAs(t, failure, &timeoutErr)
	assert.Zero(t, driver.released)
	assert.Equal(t, 1, driver.deleted)
}

func TestWorkerFailOnTimeoutDoesNotApplyToOtherErrors(t *testing.T) {
	driver, failure, _, _ := runTimeoutJob(t, WorkerOptions{}, `,"timeout":60,"failOnTimeout":true,"maxTries":5`,
		func(context.Context, *Job) error { return errors.New("ordinary failure") })

	assert.NoError(t, failure)
	assert.Equal(t, 1, driver.released)
}

func TestWorkerDefaultTimeout(t *testing.T) {
	_, _, exception, elapsed := runTimeoutJob(t, WorkerOptions{Timeout: 30 * time.Second}, `,"maxTries":5`, waitForCancel)

	assert.Equal(t, 30*time.Second, elapsed)
	var timeoutErr *TimeoutError
	assert.ErrorAs(t, exception, &timeoutErr)
}

func TestPayloadTimeoutZeroDisablesWorkerDefault(t *testing.T) {
	_, _, exception, elapsed := runTimeoutJob(t, WorkerOptions{Timeout: time.Second}, `,"timeout":0`,
		func(ctx context.Context, _ *Job) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Hour):
				return nil
			}
		})

	assert.Equal(t, time.Hour, elapsed)
	assert.NoError(t, exception)
}

func TestHandlerFinishingAfterDeadlineWithoutErrorSucceeds(t *testing.T) {
	driver, failure, exception, _ := runTimeoutJob(t, WorkerOptions{}, `,"timeout":1`,
		func(context.Context, *Job) error {
			time.Sleep(5 * time.Second) // ignores its context, but completes
			return nil
		})

	assert.NoError(t, exception)
	assert.NoError(t, failure)
	assert.Equal(t, 1, driver.deleted, "the job is acknowledged, not retried")
	assert.Zero(t, driver.released)
}

func TestNoTimeoutLeavesContextAlone(t *testing.T) {
	_, _, exception, _ := runTimeoutJob(t, WorkerOptions{}, ``,
		func(ctx context.Context, _ *Job) error {
			_, hasDeadline := ctx.Deadline()
			if hasDeadline {
				return errors.New("unexpected deadline")
			}
			return nil
		})
	assert.NoError(t, exception)
}
