package queue

import (
	"context"
	"fmt"
	"runtime/debug"
)

// PanicError is the failure recorded for a job whose handler or middleware
// panicked. The worker recovers the panic and treats it like a returned
// error, so the job is retried or failed by the usual rules and the other
// jobs keep running.
type PanicError struct {
	// Value is the value passed to panic.
	Value any
	// Stack is the stack of the panicking goroutine.
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("panic in job handler: %v", e.Value)
}

// Unwrap returns the panic value when it is an error, so errors.Is and
// errors.As see through the panic.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

// Format adds the stack trace for %+v, which is what the worker stores in
// the failed job's exception column.
func (e *PanicError) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('+') {
		fmt.Fprintf(f, "%s\n\n%s", e.Error(), e.Stack)
		return
	}
	fmt.Fprint(f, e.Error())
}

// runHandler runs the middleware pipeline and the handler under the job's
// timeout, converting a panic in either into a *PanicError.
func (w *Worker) runHandler(ctx context.Context, job *Job, handler Handler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &PanicError{Value: r, Stack: debug.Stack()}
		}
	}()
	ctx, cancel, timeoutErr := w.withJobTimeout(ctx, job)
	defer cancel()

	err = w.pipeline.Then(ctx, job, func() error {
		return handler(ctx, job)
	})
	return timedOut(ctx, timeoutErr, err)
}
