package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/events"
	"github.com/rjp2525/laravel-queue-go/pkg/failed"
	"github.com/rjp2525/laravel-queue-go/pkg/middleware"
)

type Handler func(ctx context.Context, job *Job) error

type Worker struct {
	driver         Driver
	options        WorkerOptions
	handlers       map[string]Handler
	defaultHandler Handler
	pipeline       *middleware.Pipeline
	events         *events.Bus
	failed         failed.Logger
	logger         *slog.Logger
	connection     string

	// fetchCancel cancels the context used by the fetch loop (driver.Pop and
	// the inter-poll sleep). Stop sets stopRequested and calls fetchCancel so
	// the worker stops claiming new jobs without aborting handlers that are
	// already running on the parent context passed to Run. stopRequested is
	// sticky across Run lifetimes so a Stop() before Run() makes the next Run
	// exit immediately.
	stopMu        sync.Mutex
	stopRequested bool
	fetchCancel   context.CancelFunc
}

func newWorker(
	driver Driver,
	opts WorkerOptions,
	handlers map[string]Handler,
	defaultHandler Handler,
	mw []middleware.Middleware,
	bus *events.Bus,
	fp failed.Logger,
	logger *slog.Logger,
	connection string,
) *Worker {
	if opts.Queue == "" {
		opts.Queue = DefaultQueueName
	}
	if opts.Sleep == 0 {
		opts.Sleep = 3 * time.Second
	}

	h := make(map[string]Handler, len(handlers))
	for k, v := range handlers {
		h[k] = v
	}

	return &Worker{
		driver:         driver,
		options:        opts,
		handlers:       h,
		defaultHandler: defaultHandler,
		pipeline:       middleware.NewPipeline(mw...),
		events:         bus,
		failed:         fp,
		logger:         logger,
		connection:     connection,
	}
}

// NewWorker creates a standalone worker without a Manager.
func NewWorker(driver Driver, opts WorkerOptions) *Worker {
	return newWorker(
		driver, opts,
		make(map[string]Handler), nil, nil,
		events.NewBus(), failed.NullProvider{},
		slog.Default(), DefaultConnection,
	)
}

func (w *Worker) Register(jobName string, handler Handler) { w.handlers[jobName] = handler }
func (w *Worker) RegisterDefault(handler Handler)          { w.defaultHandler = handler }
func (w *Worker) Use(m ...middleware.Middleware)           { w.pipeline.Use(m...) }
func (w *Worker) SetFailedProvider(p failed.Logger)        { w.failed = p }
func (w *Worker) SetLogger(l *slog.Logger)                 { w.logger = l }

func (w *Worker) On(eventType events.EventType, listener events.Listener) {
	w.events.On(eventType, listener)
}

// Run starts the worker loop. Blocks until the parent context is cancelled,
// Stop is called, or worker limits (MaxJobs / MaxTime) are reached. When Stop
// is the trigger, in-flight handlers keep running on the parent context until
// they finish; Run only returns after they drain. Callers that want a hard
// deadline for that drain should cancel the parent context after Stop.
func (w *Worker) Run(ctx context.Context) error {
	fetchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	w.stopMu.Lock()
	w.fetchCancel = cancel
	alreadyStopped := w.stopRequested
	w.stopMu.Unlock()

	if alreadyStopped {
		cancel()
	}

	if w.options.Concurrency <= 1 {
		return w.runSingle(ctx, fetchCtx)
	}
	return w.runConcurrent(ctx, fetchCtx)
}

// Stop signals the worker to stop pulling new jobs. In-flight handlers continue
// running on the context that was passed to Run; the call returns immediately
// without waiting for them. Safe to call multiple times and before Run starts;
// in the latter case the next Run will exit as soon as it enters its fetch loop.
func (w *Worker) Stop() {
	w.stopMu.Lock()
	defer w.stopMu.Unlock()

	w.stopRequested = true
	if w.fetchCancel != nil {
		w.fetchCancel()
	}
}

type fetchResult int

const (
	fetchGotJob fetchResult = iota
	fetchEmpty
	fetchStop // context cancelled or limits reached
	fetchError
)

func (w *Worker) fetchJob(fetchCtx context.Context, jobsProcessed int, startTime time.Time) (*RawJob, fetchResult) {
	select {
	case <-fetchCtx.Done():
		w.events.Fire(events.Event{Type: events.WorkerStopping})
		return nil, fetchStop
	default:
	}

	if w.options.MaxJobs > 0 && jobsProcessed >= w.options.MaxJobs {
		return nil, fetchStop
	}
	if w.options.MaxTime > 0 && int(time.Since(startTime).Seconds()) >= w.options.MaxTime {
		return nil, fetchStop
	}

	raw, err := w.driver.Pop(fetchCtx, w.options.Queue)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, fetchStop
		}
		w.logger.Error("pop failed", "error", err)
		if !w.sleepOrStop(fetchCtx, w.options.Sleep) {
			return nil, fetchStop
		}
		return nil, fetchError
	}

	if raw == nil {
		if w.options.StopOnEmpty {
			return nil, fetchStop
		}
		if !w.sleepOrStop(fetchCtx, w.options.Sleep) {
			return nil, fetchStop
		}
		return nil, fetchEmpty
	}

	return raw, fetchGotJob
}

// sleepOrStop sleeps for d but returns false immediately if fetchCtx is
// cancelled. Replaces bare time.Sleep so Stop() doesn't have to wait out the
// inter-poll backoff before the worker actually exits.
func (w *Worker) sleepOrStop(fetchCtx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-fetchCtx.Done():
		return false
	}
}

func (w *Worker) runSingle(ctx, fetchCtx context.Context) error {
	var jobsProcessed int
	startTime := time.Now()

	for {
		raw, result := w.fetchJob(fetchCtx, jobsProcessed, startTime)
		switch result {
		case fetchStop:
			return ctx.Err()
		case fetchEmpty, fetchError:
			continue
		}
		w.process(ctx, raw)
		jobsProcessed++
	}
}

func (w *Worker) runConcurrent(ctx, fetchCtx context.Context) error {
	var (
		wg            sync.WaitGroup
		jobsProcessed int
		mu            sync.Mutex
		startTime     = time.Now()
	)

	sem := make(chan struct{}, w.options.Concurrency)

	for {
		mu.Lock()
		count := jobsProcessed
		mu.Unlock()

		raw, result := w.fetchJob(fetchCtx, count, startTime)
		switch result {
		case fetchStop:
			wg.Wait()
			return ctx.Err()
		case fetchEmpty, fetchError:
			continue
		}

		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			w.process(ctx, raw)
			mu.Lock()
			jobsProcessed++
			mu.Unlock()
		}()
	}
}

func (w *Worker) process(ctx context.Context, raw *RawJob) {
	start := time.Now()

	job, err := ParseJob(ctx, raw, w.driver)
	if err != nil {
		w.logger.Error("failed to parse job", "error", err)
		_ = w.driver.Delete(ctx, raw)
		return
	}

	w.events.Fire(events.Event{
		Type:    events.JobProcessing,
		JobUUID: job.UUID(),
		JobName: job.DisplayName(),
		Queue:   job.Queue(),
		Attempt: job.Attempts(),
	})

	handler := w.resolveHandler(job)
	if handler == nil {
		w.logger.Warn("no handler registered", "job", job.DisplayName())
		_ = job.Delete()
		return
	}

	err = w.pipeline.Then(ctx, job, func() error {
		return handler(ctx, job)
	})

	duration := time.Since(start)

	if err != nil {
		w.handleFailure(ctx, job, err, duration)
		return
	}

	if deleteErr := job.Delete(); deleteErr != nil {
		w.logger.Error("failed to delete job", "job", job.DisplayName(), "error", deleteErr)
	}

	w.events.Fire(events.Event{
		Type:     events.JobProcessed,
		JobUUID:  job.UUID(),
		JobName:  job.DisplayName(),
		Queue:    job.Queue(),
		Attempt:  job.Attempts(),
		Duration: duration,
	})
}

func (w *Worker) handleFailure(ctx context.Context, job *Job, jobErr error, duration time.Duration) {
	w.events.Fire(events.Event{
		Type:    events.JobExceptionOccurred,
		JobUUID: job.UUID(),
		JobName: job.DisplayName(),
		Queue:   job.Queue(),
		Attempt: job.Attempts(),
		Error:   jobErr,
	})

	var releaseErr *middleware.ReleaseError
	if errors.As(jobErr, &releaseErr) {
		if err := job.Release(releaseErr.Delay); err != nil {
			w.logger.Error("failed to release job", "job", job.DisplayName(), "error", err)
		}
		return
	}

	if !job.HasFailed() {
		delay := job.BackoffDuration()
		if err := job.Release(delay); err != nil {
			w.logger.Error("failed to release job", "job", job.DisplayName(), "error", err)
		}
		w.events.Fire(events.Event{
			Type:    events.JobReleasedAfterError,
			JobUUID: job.UUID(),
			JobName: job.DisplayName(),
			Queue:   job.Queue(),
			Error:   jobErr,
		})
		return
	}

	if err := job.Delete(); err != nil {
		w.logger.Error("failed to delete failed job", "job", job.DisplayName(), "error", err)
	}

	if logErr := w.failed.Log(ctx, failed.Record{
		UUID:       job.UUID(),
		Connection: w.connection,
		Queue:      job.Queue(),
		Payload:    []byte(job.Raw().Body),
		Exception:  fmt.Sprintf("%+v", jobErr),
	}); logErr != nil {
		w.logger.Error("failed to log failed job", "job", job.DisplayName(), "error", logErr)
	}

	w.events.Fire(events.Event{
		Type:     events.JobFailed,
		JobUUID:  job.UUID(),
		JobName:  job.DisplayName(),
		Queue:    job.Queue(),
		Attempt:  job.Attempts(),
		Error:    jobErr,
		Duration: duration,
	})

	w.logger.Error("job failed permanently",
		"job", job.DisplayName(),
		"uuid", job.UUID(),
		"attempts", job.Attempts(),
		"error", jobErr,
	)
}

func (w *Worker) resolveHandler(job *Job) Handler {
	if h, ok := w.handlers[job.CommandName()]; ok {
		return h
	}
	if h, ok := w.handlers[job.DisplayName()]; ok {
		return h
	}
	return w.defaultHandler
}
