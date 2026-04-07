package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubJob implements the Job interface for testing.
type stubJob struct {
	timeout *int
}

func (s stubJob) UUID() string        { return "test-uuid" }
func (s stubJob) DisplayName() string { return "TestJob" }
func (s stubJob) CommandName() string { return "App\\Jobs\\Test" }
func (s stubJob) Attempts() int       { return 1 }
func (s stubJob) Queue() string       { return "default" }
func (s stubJob) MaxTries() *int      { return nil }
func (s stubJob) Timeout() *int       { return s.timeout }
func (s stubJob) Tags() []string      { return nil }
func (s stubJob) BatchID() string     { return "" }

var testJob Job = stubJob{}

func TestPipelineExecutionOrder(t *testing.T) {
	var order []string

	m1 := func(ctx context.Context, job Job, next func() error) error {
		order = append(order, "m1-before")
		err := next()
		order = append(order, "m1-after")
		return err
	}
	m2 := func(ctx context.Context, job Job, next func() error) error {
		order = append(order, "m2-before")
		err := next()
		order = append(order, "m2-after")
		return err
	}

	pipe := NewPipeline(m1, m2)
	err := pipe.Then(context.Background(), testJob, func() error {
		order = append(order, "handler")
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{
		"m1-before",
		"m2-before",
		"handler",
		"m2-after",
		"m1-after",
	}, order)
}

func TestPipelineNoMiddleware(t *testing.T) {
	called := false
	pipe := NewPipeline()
	err := pipe.Then(context.Background(), testJob, func() error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestPipelinePropagatesError(t *testing.T) {
	m := func(ctx context.Context, job Job, next func() error) error {
		return next()
	}
	pipe := NewPipeline(m)
	expectedErr := errors.New("handler failed")
	err := pipe.Then(context.Background(), testJob, func() error {
		return expectedErr
	})
	assert.ErrorIs(t, err, expectedErr)
}

func TestPipelineMiddlewareShortCircuit(t *testing.T) {
	handlerCalled := false
	blocker := func(ctx context.Context, job Job, next func() error) error {
		return errors.New("blocked")
	}
	pipe := NewPipeline(blocker)
	err := pipe.Then(context.Background(), testJob, func() error {
		handlerCalled = true
		return nil
	})
	assert.Error(t, err)
	assert.False(t, handlerCalled)
}

func TestPipelineUse(t *testing.T) {
	var order []string
	pipe := NewPipeline()
	pipe.Use(func(ctx context.Context, job Job, next func() error) error {
		order = append(order, "added")
		return next()
	})
	err := pipe.Then(context.Background(), testJob, func() error {
		order = append(order, "handler")
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"added", "handler"}, order)
}

func TestTimeoutMiddlewareExpires(t *testing.T) {
	dur := 50 * time.Millisecond
	getter := func(job Job) *time.Duration {
		return &dur
	}
	mw := Timeout(getter)
	pipe := NewPipeline(mw)

	err := pipe.Then(context.Background(), testJob, func() error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestTimeoutMiddlewareNoTimeout(t *testing.T) {
	getter := func(job Job) *time.Duration {
		return nil
	}
	mw := Timeout(getter)
	pipe := NewPipeline(mw)

	called := false
	err := pipe.Then(context.Background(), testJob, func() error {
		called = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestTimeoutMiddlewareCompletesBeforeDeadline(t *testing.T) {
	dur := 5 * time.Second
	getter := func(job Job) *time.Duration {
		return &dur
	}
	mw := Timeout(getter)
	pipe := NewPipeline(mw)

	err := pipe.Then(context.Background(), testJob, func() error {
		return nil
	})

	require.NoError(t, err)
}
