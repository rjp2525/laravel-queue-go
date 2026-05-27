package queue

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopTestDriver is a Driver that hands out one job on the first Pop and then
// blocks every subsequent Pop until its context is cancelled. That lets a test
// pin a handler in flight while calling Stop and observe that:
//   - Pop returns immediately when Stop cancels the fetch context
//   - the in-flight handler keeps its original context (not cancelled by Stop)
//   - Run blocks until the handler finishes, then returns
type stopTestDriver struct {
	served        atomic.Bool
	popReturned   chan struct{}
	popReturnOnce sync.Once
}

func newStopTestDriver() *stopTestDriver {
	return &stopTestDriver{popReturned: make(chan struct{})}
}

func (d *stopTestDriver) Push(context.Context, string, []byte) (string, error) {
	return "", nil
}
func (d *stopTestDriver) Later(context.Context, string, time.Duration, []byte) (string, error) {
	return "", nil
}
func (d *stopTestDriver) Pop(ctx context.Context, queue string) (*RawJob, error) {
	if !d.served.Swap(true) {
		p := NewPayload(`App\Jobs\Slow`, `O:4:"Slow":0:{}`)
		body, _ := json.Marshal(p)
		return &RawJob{Body: string(body), Queue: queue, ReservedUntil: time.Now().Add(60 * time.Second)}, nil
	}
	<-ctx.Done()
	d.popReturnOnce.Do(func() { close(d.popReturned) })
	return nil, ctx.Err()
}
func (d *stopTestDriver) Delete(context.Context, *RawJob) error                 { return nil }
func (d *stopTestDriver) Release(context.Context, *RawJob, time.Duration) error { return nil }
func (d *stopTestDriver) Size(context.Context, string) (int64, error)           { return 0, nil }
func (d *stopTestDriver) Clear(context.Context, string) (int64, error)          { return 0, nil }
func (d *stopTestDriver) MigrateExpiredJobs(context.Context, string) error      { return nil }
func (d *stopTestDriver) Close() error                                          { return nil }

// TestWorkerStopDrainsInFlight is the regression test for the SIGTERM-mid-job
// behavior that left reserved jobs stranded for retry_after seconds. Stop()
// must cancel the fetch loop but leave handler contexts alive, and Run() must
// return only after the in-flight handler completes.
func TestWorkerStopDrainsInFlight(t *testing.T) {
	driver := newStopTestDriver()
	worker := NewWorker(driver, WorkerOptions{
		Queue:       "stop-test",
		Concurrency: 2,
		Sleep:       10 * time.Millisecond,
	})

	handlerStarted := make(chan struct{})
	var completedNormally atomic.Bool
	worker.Register(`App\Jobs\Slow`, func(ctx context.Context, job *Job) error {
		close(handlerStarted)
		select {
		case <-time.After(400 * time.Millisecond):
			completedNormally.Store(true)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	runErr := make(chan error, 1)
	go func() { runErr <- worker.Run(context.Background()) }()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	stopAt := time.Now()
	worker.Stop()

	select {
	case <-driver.popReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("fetch loop did not exit Pop within 2s after Stop")
	}

	select {
	case err := <-runErr:
		drain := time.Since(stopAt)
		assert.NoError(t, err, "Run should return cleanly after graceful Stop")
		assert.True(t, completedNormally.Load(),
			"handler must complete normally; Stop must not cancel its context")
		assert.GreaterOrEqual(t, drain, 250*time.Millisecond,
			"Run must block until in-flight handler finishes (≈400ms job)")
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Stop within 2s")
	}
}

// TestWorkerStopIdempotent confirms multiple Stop() calls and pre-Run Stop()
// are safe.
func TestWorkerStopIdempotent(t *testing.T) {
	worker := NewWorker(newStopTestDriver(), WorkerOptions{Queue: "x", Concurrency: 1})
	require.NotPanics(t, func() {
		worker.Stop()
		worker.Stop()
	})

	worker2 := NewWorker(newStopTestDriver(), WorkerOptions{Queue: "x", Concurrency: 1})
	worker2.Stop()
	runErr := make(chan error, 1)
	go func() { runErr <- worker2.Run(context.Background()) }()
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit immediately when Stop was called before Run")
	}
}
