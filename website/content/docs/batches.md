---
title: Batches
description: Read and update Laravel's job_batches table from Go, and what to expect when Go handles jobs that belong to a batch.
---

Laravel batches (`Bus::batch([...])`) track their progress in the `job_batches` table. This package can read and update that table. It does not run a batch's callbacks, and the worker doesn't update batch counters on its own. This page shows how to do that yourself and where the edges are.

## Is this job part of a batch?

```go
if id := job.BatchID(); id != "" {
    // dispatched through Bus::batch
}
```

## The repository

```go
import "github.com/rjp2525/laravel-queue-go/pkg/batch"

repo, err := batch.NewRepository(db, "job_batches")
```

It speaks MySQL, PostgreSQL, SQLite and SQL Server, detected from your driver (pass `batch.WithDialect(...)` for wrapped drivers).

```go
b, err := repo.Find(ctx, id)

b.Name
b.TotalJobs
b.PendingJobs
b.FailedJobs
b.Progress()        // 0 to 100
b.Cancelled()
b.Finished()
b.AllowsFailures()  // the batch was created with ->allowFailures()
```

## Keeping the counters right

Laravel's worker decrements `pending_jobs` when a batched job succeeds and records the failure when one fails for good. When a Go worker runs the job, that's on you.

```go
mgr.Register(`App\Jobs\ImportRow`, func(ctx context.Context, job *queue.Job) error {
    batchID := job.BatchID()

    // Laravel skips jobs of a cancelled batch; do the same.
    if batchID != "" {
        cancelled, err := repo.IsCancelled(ctx, batchID)
        if err != nil {
            return err
        }
        if cancelled {
            return nil
        }
    }

    if err := importRow(ctx, job); err != nil {
        return err
    }

    if batchID != "" {
        if _, err := repo.DecrementPendingJobs(ctx, batchID); err != nil {
            log.Printf("batch %s: could not record success: %v", batchID, err)
        }
    }
    return nil
})
```

Record final failures from an event listener, since that's the one place that knows a job is out of retries. The event doesn't carry the batch id, so keep a small map from job UUID to batch id, or look it up from your own data:

```go
mgr.On(events.JobFailed, func(e events.Event) {
    if batchID, ok := batchOf(e.JobUUID); ok {
        _ = repo.IncrementFailedJobs(context.Background(), batchID, e.JobUUID)
    }
})
```

When `pending_jobs` reaches zero the batch is marked finished.

> [!WARNING]
> A batch's `then`, `catch` and `finally` callbacks are serialized PHP closures. Only a Laravel worker can run them, and it runs them when *it* processes the job that completes the batch. If the last job of a batch is handled by Go, the batch is marked finished in the table but the callbacks do not fire.

That limit shapes how to use batches with Go. Two approaches that work well:

- **Let PHP own the ending.** Add a small final PHP job to the batch, or check `Bus::findBatch($id)->finished()` from a scheduled command, and do the follow-up work there.
- **Don't batch across languages.** Keep the batch all-PHP and have one of its jobs dispatch the Go work separately.

## Creating and managing batches from Go

For batches that Go owns end to end:

```go
b, err := repo.Create(ctx, batch.CreateOptions{
    Name:          "nightly-import",
    TotalJobs:     500,
    AllowFailures: true,
})

err = repo.Cancel(ctx, b.ID)
pruned, err := repo.Prune(ctx, 72*time.Hour) // delete batches finished over 3 days ago
```

Batches created here appear in Laravel (`Bus::findBatch`, Horizon's batch list) with their name and counts. They have no PHP callbacks.
