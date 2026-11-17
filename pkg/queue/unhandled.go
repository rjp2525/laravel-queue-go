package queue

import (
	"context"
	"errors"
	"fmt"

	"github.com/rjp2525/laravel-queue-go/v2/pkg/events"
	"github.com/rjp2525/laravel-queue-go/v2/pkg/failed"
)

// ErrNoHandler is the failure recorded for a job that no registered handler
// (and no default handler) accepts.
var ErrNoHandler = errors.New("no handler registered")

// failUnparseable removes a job whose payload could not be decoded and
// records it as failed, so that it is not lost without a trace.
func (w *Worker) failUnparseable(ctx context.Context, raw *RawJob, parseErr error) {
	if err := w.driver.Delete(ctx, raw); err != nil {
		w.logger.Error("failed to delete unparseable job", "queue", raw.Queue, "error", err)
	}

	if err := w.failed.Log(ctx, failed.Record{
		Connection: w.connection,
		Queue:      raw.Queue,
		Payload:    []byte(raw.Body),
		Exception:  fmt.Sprintf("%+v", parseErr),
	}); err != nil {
		w.logger.Error("failed to log unparseable job", "queue", raw.Queue, "error", err)
	}

	w.events.Fire(events.Event{
		Type:  events.JobFailed,
		Queue: raw.Queue,
		Error: parseErr,
	})

	w.logger.Error("job failed permanently: payload could not be parsed",
		"queue", raw.Queue,
		"error", parseErr,
	)
}
