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

// Run starts the worker loop. Blocks until context is cancelled or limits are reached.
func (w *Worker) Run(ctx context.Context) error {
	if w.options.Concurrency <= 1 {
		return w.runSingle(ctx)
	}
	return w.runConcurrent(ctx)
}

type fetchResult int

const (
	fetchGotJob fetchResult = iota
	fetchEmpty
	fetchStop // context cancelled or limits reached
	fetchError
)

func (w *Worker) fetchJob(ctx context.Context, jobsProcessed int, startTime time.Time) (*RawJob, fetchResult) {
	select {
	case <-ctx.Done():
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

	raw, err := w.driver.Pop(ctx, w.options.Queue)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, fetchStop
		}
		w.logger.Error("pop failed", "error", err)
		time.Sleep(w.options.Sleep)
		return nil, fetchError
	}

	if raw == nil {
		if w.options.StopOnEmpty {
			return nil, fetchStop
		}
		time.Sleep(w.options.Sleep)
		return nil, fetchEmpty
	}

	return raw, fetchGotJob
}

func (w *Worker) runSingle(ctx context.Context) error {
	var jobsProcessed int
	startTime := time.Now()

	for {
		raw, result := w.fetchJob(ctx, jobsProcessed, startTime)
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

func (w *Worker) runConcurrent(ctx context.Context) error {
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

		raw, result := w.fetchJob(ctx, count, startTime)
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
