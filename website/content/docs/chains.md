---
title: Job chains
description: Dispatch a sequence of jobs that run one after another and what to know when Go handles part of a chain.
---

A chain is a list of jobs that run in order. Each one starts only after the previous one succeeded. If one fails, the rest never run.

## Dispatching a chain from Go

```go
d.DispatchWithOptions(ctx, queue.DispatchOptions{
    Job:  `App\Jobs\ProcessPodcast`,
    Args: map[string]any{"podcastId": 7},
    Chain: []queue.ChainedJob{
        {Job: `App\Jobs\OptimizePodcast`, Args: map[string]any{"podcastId": 7}},
        {Job: `App\Jobs\ReleasePodcast`, Args: map[string]any{"podcastId": 7}},
    },
})
```

This is the equivalent of:

```php
Bus::chain([
    new ProcessPodcast(7),
    new OptimizePodcast(7),
    new ReleasePodcast(7),
])->dispatch();
```

Laravel's workers run it like any other chain: when `ProcessPodcast` succeeds, the worker dispatches `OptimizePodcast` and so on down the list. The job classes need the `Queueable` trait, which every generated job has.

## When a Go worker handles a chained job

This is the part to know about. **Laravel's worker continues a chain. A Go worker does not.** If a Go handler processes a job that has more jobs chained after it, the handler has to dispatch the next one or the chain stops there.

You can see what's waiting:

```go
mgr.Register(`App\Jobs\ProcessPodcast`, func(ctx context.Context, job *queue.Job) error {
    if err := process(ctx, job.GetInt("podcastId")); err != nil {
        return err
    }

    for _, next := range job.ChainedJobs() {
        // each entry is a PHP-serialized command
        decoded, err := phpserialize.Decode(next)
        if err != nil {
            return err
        }
        obj := decoded.(*phpserialize.Object)
        log.Printf("chained: %s", obj.ClassName)
    }
    return nil
})
```

In practice the simplest designs avoid the problem:

- **Put the Go step last.** PHP runs the earlier steps and continues the chain. The Go job at the end has nothing after it.
- **Have the Go handler dispatch what comes next explicitly**, with `d.Dispatch`, instead of relying on the chain.
- **Keep the whole chain on one side.** A chain made only of PHP jobs, dispatched from Go, works without caveats.

> [!NOTE]
> Chain callbacks (`->catch(...)`) are PHP closures. They run when a Laravel worker handles the failing job. They can't run in Go.
