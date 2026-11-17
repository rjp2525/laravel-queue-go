---
title: laravel-queue-go
description: Run Go workers on your Laravel Redis queues. Same jobs, same retries, same failed-jobs table, for Laravel 10 through 13.
---

```php
// app/Jobs/ResizeImage.php, dispatched like any other job
ResizeImage::dispatch($photo->id, 1600)
    ->onQueue('images');
```

```go
// worker/main.go
mgr.Register(`App\Jobs\ResizeImage`, resizeImage)

func resizeImage(ctx context.Context, job *queue.Job) error {
    photoID := job.GetInt("photoId")
    width := job.GetInt("width")

    // return an error and it retries with backoff
    return resize(ctx, photoID, width)
}

worker := mgr.Worker(queue.WorkerOptions{
    Queue:       "images",
    Concurrency: 8,
})
worker.Run(ctx)
```
