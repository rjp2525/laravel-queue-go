package queue

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TimeoutError is the failure recorded for a job that was still running when
// its timeout expired and then returned an error.
type TimeoutError struct {
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("job timed out after %s", e.Timeout)
}

// timeoutFor returns how long the job may run: the payload's "timeout" in
// seconds when it has one (0 disables the timeout), else the worker default.
func (w *Worker) timeoutFor(job *Job) time.Duration {
	if t := job.payload.Timeout; t != nil {
		return time.Duration(*t) * time.Second
	}
	return w.options.Timeout
}

// withJobTimeout derives the context the middleware and handler run on. When
// the job has a timeout, the context is cancelled at the deadline with the
// returned *TimeoutError as its cause.
func (w *Worker) withJobTimeout(ctx context.Context, job *Job) (context.Context, context.CancelFunc, *TimeoutError) {
	timeout := w.timeoutFor(job)
	if timeout <= 0 {
		return ctx, func() {}, nil
	}
	timeoutErr := &TimeoutError{Timeout: timeout}
	ctx, cancel := context.WithTimeoutCause(ctx, timeout, timeoutErr)
	return ctx, cancel, timeoutErr
}

// timedOut attributes a handler error to the timeout when the deadline is
// what cancelled the context. A handler that finishes without error after
// the deadline has still done its work and counts as a success.
func timedOut(ctx context.Context, timeoutErr *TimeoutError, err error) error {
	if err == nil || timeoutErr == nil || !errors.Is(context.Cause(ctx), timeoutErr) {
		return err
	}
	return fmt.Errorf("%w: %w", timeoutErr, err)
}

// failsOnTimeout reports whether jobErr is a timeout of a job that asked to
// be failed, rather than retried, when it times out ("failOnTimeout").
func failsOnTimeout(job *Job, jobErr error) bool {
	var timeoutErr *TimeoutError
	return job.payload.FailOnTimeout && errors.As(jobErr, &timeoutErr)
}
