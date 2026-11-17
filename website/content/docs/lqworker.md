---
title: The lqworker binary
description: A small command-line worker for checking your setup. Read the warning before you point it at anything real.
---

`lqworker` is a ready-made worker binary. It connects, reserves jobs and logs what it sees. It's useful for one thing: proving that your Go side can see the jobs your Laravel side dispatches, before you've written any code.

> [!WARNING]
> `lqworker` has no job handlers. It logs each job it receives and **acknowledges it, which removes the job from the queue**. Run it against a test queue. Never run it against a queue whose jobs you need.

## Install

```bash
go install github.com/rjp2525/laravel-queue-go/v2/cmd/lqworker@latest
```

Prebuilt binaries for Linux, macOS and Windows are attached to each [GitHub release](https://github.com/rjp2525/laravel-queue-go/releases).

## A connectivity check

Dispatch a job to a scratch queue from Laravel:

```php
App\Jobs\Ping::dispatch()->onQueue('lqworker-test');
```

Then:

```bash
lqworker --queue=lqworker-test --prefix=laravel-database- --max-jobs=1
```

You should see the job logged as JSON with its class name, UUID and attempt number. Then the process exits. If it sits there, the prefix is wrong or the job went to a different Redis database.

## Flags

```text
--queue              queue to consume (default "default")
--redis              Redis address, host:port
--redis-username     Redis ACL username
--redis-password     Redis password
--redis-db           Redis database number
--prefix             Redis key prefix
--laravel-version    10, 11, 12, 13 or auto
--concurrency        jobs to run at once
--sleep              pause when the queue is empty
--retry-after        time before a reserved job is handed out again
--block-for          time to wait on Redis for a job
--max-jobs           exit after this many jobs
--max-time           exit after this many seconds
--shutdown-timeout   time to let running jobs finish on SIGTERM (0 = as long as it takes)
--version            print the version
```

Time flags take seconds like `queue:work` does (`--sleep=3`) or a Go duration (`--sleep=500ms`).

Every setting can also come from the [environment](/docs/configuration/#variables). Flags win. The binary refuses to start if an environment value is invalid and tells you which one.

## Shutdown

On the first `SIGTERM` or `SIGINT`, `lqworker` stops taking jobs and waits for the running ones. A second signal cancels them. So does `--shutdown-timeout` running out.

## For real workloads

Write your own `main`. It's about thirty lines and the [quick start](/docs/quick-start/) has all of them. It's also the only way to register handlers. `lqworker`'s source in `cmd/lqworker` is a reasonable starting point for flag handling and shutdown.
