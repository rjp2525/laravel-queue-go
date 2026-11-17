---
title: Events
description: Hook into the job lifecycle for logging, metrics and alerting.
---

The worker announces what it's doing. Listen in to feed your logs, metrics or pager.

```go
import "github.com/rjp2525/laravel-queue-go/v2/pkg/events"

mgr.On(events.JobProcessed, func(e events.Event) {
    metrics.Timing("jobs.duration", e.Duration, "job:"+e.JobName)
})

mgr.On(events.JobFailed, func(e events.Event) {
    alert.Send(fmt.Sprintf("%s failed after %d attempts: %v", e.JobName, e.Attempt, e.Error))
})
```

## The events

| Event | Fires when | Laravel's equivalent |
|---|---|---|
| `events.JobProcessing` | A job is about to run | `JobProcessing` |
| `events.JobProcessed` | A handler returned `nil` | `JobProcessed` |
| `events.JobExceptionOccurred` | A handler returned an error or panicked | `JobExceptionOccurred` |
| `events.JobReleasedAfterError` | A failed attempt was put back for a retry | `JobReleasedAfterException` |
| `events.JobFailed` | A job is out of attempts and has been failed | `JobFailed` |
| `events.WorkerStopping` | `Run` is about to return | `WorkerStopping` |

A failing job that still has retries left produces `JobExceptionOccurred` then `JobReleasedAfterError`. On its last attempt it produces `JobExceptionOccurred` then `JobFailed`.

## What an event carries

```go
type Event struct {
    Type       EventType
    JobUUID    string
    JobName    string        // the display name, usually the class
    Queue      string
    Attempt    int           // 1 on the first run
    Duration   time.Duration // set on JobProcessed and JobFailed
    Error      error         // set on the failure events
    Connection string        // reserved; not populated yet
}
```

`JobFailed` can arrive without a `JobUUID` or `JobName`. That happens when the payload couldn't be parsed at all, so there was nothing to read them from. `Queue` and `Error` are always there.

## Listeners run inline

A listener is called on the goroutine that's processing the job, before the worker moves on. That keeps ordering simple, and it means two things:

- **Keep them quick.** A listener that makes a slow network call slows every job. Hand slow work to a channel or a goroutine.
- **Make them concurrency-safe.** With `Concurrency` above 1, the same listener is called from several goroutines at once.

`WorkerStopping` fires once per `Run`, after in-flight jobs have finished, whatever the reason: `Stop()`, a cancelled context, `MaxJobs`, `MaxTime` or an empty queue with `StopOnEmpty`. It's the right place to flush metrics.

## A Prometheus-shaped example

```go
var (
    processed = promauto.NewCounterVec(prometheus.CounterOpts{Name: "queue_jobs_total"}, []string{"job", "result"})
    duration  = promauto.NewHistogramVec(prometheus.HistogramOpts{Name: "queue_job_seconds"}, []string{"job"})
)

mgr.On(events.JobProcessed, func(e events.Event) {
    processed.WithLabelValues(e.JobName, "ok").Inc()
    duration.WithLabelValues(e.JobName).Observe(e.Duration.Seconds())
})
mgr.On(events.JobReleasedAfterError, func(e events.Event) {
    processed.WithLabelValues(e.JobName, "retried").Inc()
})
mgr.On(events.JobFailed, func(e events.Event) {
    processed.WithLabelValues(e.JobName, "failed").Inc()
})
```

For queue depth, poll the driver: `PendingSize`, `DelayedSize` and `ReservedSize` are described in [going to production](/docs/going-to-production/#watch-the-queue).
