---
title: Introduction
description: What this package is, when it earns its keep and what it deliberately does not try to do.
---

`laravel-queue-go` lets a Go program take part in a Laravel Redis queue. It can **consume** jobs your Laravel app dispatches. It can also **dispatch** jobs for `php artisan queue:work` to run.

Nothing changes on the Laravel side. You keep writing job classes and calling `dispatch()`. A Go worker reads the same Redis keys Laravel does, runs the same Lua scripts and records failures in the same `failed_jobs` table.

## When it's worth it

Most jobs are fine in PHP. Reach for a Go worker when one of them isn't:

- **CPU-heavy work.** Image and video processing, PDF rendering, large imports, anything where a PHP worker pins a core for seconds.
- **High fan-out I/O.** Thousands of webhooks or API calls, where one Go process with a few hundred goroutines replaces a fleet of `queue:work` processes.
- **Work that already lives in Go.** You have a Go library or service that does the thing. Wrapping it in an HTTP endpoint just so PHP can call it feels silly.
- **Memory.** A Go worker with 50 concurrent jobs is one process. Fifty PHP workers are fifty copies of your framework in RAM.

You don't have to move everything. Put the expensive job class on its own queue, point a Go worker at that queue and leave the rest alone.

## How it fits together

```text
Laravel app                    Redis                        Go worker
-----------                    -----                        ---------
ResizeImage::dispatch()  --->  queues:images          --->  handler(ctx, job)
                               queues:images:reserved        |
                               queues:images:delayed  <---   retry with backoff
failed_jobs table        <-------------------------------    out of attempts
```

The Go side implements Laravel's queue protocol rather than inventing a bridge. That has a practical consequence: Go workers and PHP workers can run on the **same queue at the same time**. A job dispatched from Go is indistinguishable from one dispatched by Laravel.

## What you get

- A worker with bounded concurrency, graceful shutdown, panic recovery and per-job timeouts.
- Typed access to the job's data, including PHP enums and Eloquent model identifiers.
- Laravel's retry rules: `$tries`, `$backoff`, `retryUntil`, `$timeout`, `failOnTimeout`.
- A dispatcher for immediate jobs, delayed jobs and chains.
- Failed job storage and batch bookkeeping against Laravel's own tables, on MySQL, PostgreSQL, SQLite or SQL Server.
- Support for Laravel 10, 11, 12 and 13, selected explicitly or [detected from the queue](/docs/laravel-versions/).

## What it doesn't do

Being upfront saves you an afternoon:

- **Redis only.** The `database`, `sqs` and `beanstalkd` queue drivers are not supported.
- **It doesn't run your PHP.** A Go handler gets the job's *data*. The `handle()` method, the service container and your Eloquent models stay in PHP. You write the Go version of the work.
- **No encrypted jobs or closures.** `ShouldBeEncrypted` payloads and queued closures can't be read from Go.

The full list is on the [limitations](/docs/limitations/) page. If none of that is a dealbreaker, [install it](/docs/installation/) and have a job running in a few minutes.
