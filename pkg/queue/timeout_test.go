package queue

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rjp2525/laravel-queue-go/v2/pkg/events"
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

// timeoutRun is what happened to the one job a test worker processed.
type timeoutRun struct {
	driver    *panicTestDriver
	failure   error // from JobFailed
	exception error // from JobExceptionOccurred
	elapsed   time.Duration
}

func runTimeoutJob(t *testing.T, opts WorkerOptions, payloadExtra string, handler Handler) timeoutRun {
	t.Helper()
	var (
		driver             *panicTestDriver
		failure, exception error
		elapsed            time.Duration
	)
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
	return timeoutRun{driver: driver, failure: failure, exception: exception, elapsed: elapsed}
}

func TestWorkerEnforcesPayloadTimeout(t *testing.T) {
	run := runTimeoutJob(t, WorkerOptions{}, `,"timeout":2,"maxTries":5`, waitForCancel)

	assert.Equal(t, 2*time.Second, run.elapsed, "the handler's context is cancelled at the payload timeout")

	var timeoutErr *TimeoutError
	require.ErrorAs(t, run.exception, &timeoutErr)
	assert.Equal(t, 2*time.Second, timeoutErr.Timeout)
	assert.ErrorIs(t, run.exception, context.DeadlineExceeded, "the handler's own error is kept")

	assert.NoError(t, run.failure, "a timed-out attempt is retried while tries remain")
	assert.Equal(t, 1, run.driver.released)
}

func TestWorkerFailOnTimeoutFailsWithoutRetry(t *testing.T) {
	run := runTimeoutJob(t, WorkerOptions{}, `,"timeout":1,"failOnTimeout":true,"maxTries":5`, waitForCancel)

	var timeoutErr *TimeoutError
	require.ErrorAs(t, run.failure, &timeoutErr)
	assert.Zero(t, run.driver.released)
	assert.Equal(t, 1, run.driver.deleted)
}

func TestWorkerFailOnTimeoutDoesNotApplyToOtherErrors(t *testing.T) {
	run := runTimeoutJob(t, WorkerOptions{}, `,"timeout":60,"failOnTimeout":true,"maxTries":5`,
		func(context.Context, *Job) error { return errors.New("ordinary failure") })

	assert.NoError(t, run.failure)
	assert.Equal(t, 1, run.driver.released)
}

func TestWorkerDefaultTimeout(t *testing.T) {
	run := runTimeoutJob(t, WorkerOptions{Timeout: 30 * time.Second}, `,"maxTries":5`, waitForCancel)

	assert.Equal(t, 30*time.Second, run.elapsed)
	var timeoutErr *TimeoutError
	assert.ErrorAs(t, run.exception, &timeoutErr)
}

func TestPayloadTimeoutZeroDisablesWorkerDefault(t *testing.T) {
	run := runTimeoutJob(t, WorkerOptions{Timeout: time.Second}, `,"timeout":0`,
		func(ctx context.Context, _ *Job) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Hour):
				return nil
			}
		})

	assert.Equal(t, time.Hour, run.elapsed)
	assert.NoError(t, run.exception)
}

func TestHandlerFinishingAfterDeadlineWithoutErrorSucceeds(t *testing.T) {
	run := runTimeoutJob(t, WorkerOptions{}, `,"timeout":1`,
		func(context.Context, *Job) error {
			time.Sleep(5 * time.Second) // ignores its context, but completes
			return nil
		})

	assert.NoError(t, run.exception)
	assert.NoError(t, run.failure)
	assert.Equal(t, 1, run.driver.deleted, "the job is acknowledged, not retried")
	assert.Zero(t, run.driver.released)
}

func TestNoTimeoutLeavesContextAlone(t *testing.T) {
	run := runTimeoutJob(t, WorkerOptions{}, ``,
		func(ctx context.Context, _ *Job) error {
			_, hasDeadline := ctx.Deadline()
			if hasDeadline {
				return errors.New("unexpected deadline")
			}
			return nil
		})
	assert.NoError(t, run.exception)
}
