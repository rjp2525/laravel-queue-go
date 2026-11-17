package middleware

import (
	"context"
	"fmt"
)

// Timeout returns from the pipeline once the timeout has passed.
//
// Deprecated: the worker enforces the payload's "timeout" (and
// WorkerOptions.Timeout) itself and cancels the handler's context, which this
// middleware cannot do: it only stops waiting, so the handler keeps running
// in a leaked goroutine while the job is already being retried. Use a custom
// timeout by setting it on the job or on WorkerOptions instead.
func Timeout(getter TimeoutGetter) Middleware {
	return func(ctx context.Context, job Job, next func() error) error {
		timeout := getter(job)
		if timeout == nil || *timeout == 0 {
			return next()
		}

		ctx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()

		done := make(chan error, 1)
		go func() {
			done <- next()
		}()

		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return fmt.Errorf("job timed out after %s", *timeout)
		}
	}
}
