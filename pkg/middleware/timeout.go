package middleware

import (
	"context"
	"fmt"
)

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
