package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endlessDriver always has another job and counts how many were reserved.
type endlessDriver struct {
	mockDriver
	popped atomic.Int64
	limit  int64 // 0 = endless
}

func (d *endlessDriver) Pop(context.Context, string) (*RawJob, error) {
	if d.limit > 0 && d.popped.Load() >= d.limit {
		return nil, nil
	}
	d.popped.Add(1)
	return &RawJob{
		Body:  `{"uuid":"u","displayName":"Job","job":"j","data":{"commandName":"Job","command":""},"attempts":0}`,
		Queue: "default",
	}, nil
}

func TestConcurrentWorkerReservesOnlyWhenSlotIsFree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		driver := &endlessDriver{}
		release := make(chan struct{})

		worker := NewWorker(driver, WorkerOptions{Concurrency: 2})
		worker.Register("Job", func(context.Context, *Job) error {
			<-release
			return nil
		})

		done := make(chan error, 1)
		go func() { done <- worker.Run(t.Context()) }()

		synctest.Wait() // both slots are busy, the loop is waiting for one
		assert.Equal(t, int64(2), driver.popped.Load(),
			"no third job may be reserved while both slots are busy")

		worker.Stop()
		close(release)
		require.NoError(t, <-done)
	})
}

func TestConcurrentWorkerDoesNotOvershootMaxJobs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		driver := &endlessDriver{}
		var handled atomic.Int64

		worker := NewWorker(driver, WorkerOptions{Concurrency: 4, MaxJobs: 5})
		worker.Register("Job", func(context.Context, *Job) error {
			time.Sleep(time.Second) // slow, so jobs overlap
			handled.Add(1)
			return nil
		})

		require.NoError(t, worker.Run(t.Context()))
		assert.Equal(t, int64(5), driver.popped.Load())
		assert.Equal(t, int64(5), handled.Load())
	})
}

func TestWorkerStoppingFiresOnceForEveryStopReason(t *testing.T) {
	for name, tc := range map[string]struct {
		opts   WorkerOptions
		driver *endlessDriver
		stop   func(w *Worker, cancel context.CancelFunc)
	}{
		"max jobs":            {opts: WorkerOptions{MaxJobs: 2}, driver: &endlessDriver{}},
		"max jobs concurrent": {opts: WorkerOptions{MaxJobs: 2, Concurrency: 3}, driver: &endlessDriver{}},
		"max time":            {opts: WorkerOptions{MaxTime: 3}, driver: &endlessDriver{}},
		"stop on empty":       {opts: WorkerOptions{StopOnEmpty: true}, driver: &endlessDriver{limit: 2}},
		"stop":                {opts: WorkerOptions{}, driver: &endlessDriver{limit: 1}, stop: func(w *Worker, _ context.CancelFunc) { w.Stop() }},
		"stop concurrent":     {opts: WorkerOptions{Concurrency: 3}, driver: &endlessDriver{limit: 1}, stop: func(w *Worker, _ context.CancelFunc) { w.Stop() }},
		"context cancelled":   {opts: WorkerOptions{}, driver: &endlessDriver{limit: 1}, stop: func(_ *Worker, cancel context.CancelFunc) { cancel() }},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				worker := NewWorker(tc.driver, tc.opts)
				worker.Register("Job", func(context.Context, *Job) error {
					time.Sleep(time.Second)
					return nil
				})
				var stopping atomic.Int64
				worker.On(events.WorkerStopping, func(events.Event) { stopping.Add(1) })

				done := make(chan struct{})
				go func() {
					defer close(done)
					_ = worker.Run(ctx)
				}()

				if tc.stop != nil {
					time.Sleep(10 * time.Second)
					assert.Zero(t, stopping.Load(), "not stopping yet")
					tc.stop(worker, cancel)
				}
				<-done
				assert.Equal(t, int64(1), stopping.Load())
			})
		})
	}
}
