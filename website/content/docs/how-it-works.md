---
title: How it works
description: The Redis keys, the life of a job and why Go and PHP workers can share a queue.
---

You don't need any of this to use the package. It's here for the day you're staring at `redis-cli` wondering where a job went.

## Four keys per queue

For a queue named `emails` with the prefix `laravel-database-`:

| Key | Type | Holds |
|---|---|---|
| `laravel-database-queues:emails` | list | Jobs ready to run, oldest first |
| `laravel-database-queues:emails:delayed` | sorted set | Jobs waiting for a delay or a retry backoff, scored by when they're due |
| `laravel-database-queues:emails:reserved` | sorted set | Jobs a worker is running, scored by when the reservation expires |
| `laravel-database-queues:emails:notify` | list | One token per ready job, so blocked workers wake up |

That's all of it. These are Laravel's keys. This package reads and writes them the same way Laravel does.

```bash
redis-cli LLEN   laravel-database-queues:emails
redis-cli ZCARD  laravel-database-queues:emails:delayed
redis-cli ZRANGE laravel-database-queues:emails:reserved 0 -1 WITHSCORES
```

## The life of a job

```text
            dispatch                      pop                    handler returns nil
 Laravel ------------->  ready list  ----------->  reserved  ----------------------->  gone
   or Go                     ^                        |
                             |                        |  handler returns an error
                             |   due                  v
                             +-----------------    delayed   (waits for the backoff)
                             |
                             |   reservation expired (worker died)
                             +-----------------    reserved
```

1. **Dispatch.** The payload is a JSON document. It gets pushed onto the ready list and a token goes onto `notify`. A delayed job goes into the `delayed` set instead, scored with the time it becomes due.
2. **Migrate.** Before popping, a worker moves two things back onto the ready list: anything due in `delayed` and anything in `reserved` whose reservation has expired. This runs at most once a second per queue.
3. **Pop.** A Lua script takes the first job off the ready list, adds one to its `attempts` and stores it in `reserved` with a score of now plus `retry_after`. Because it's a script, no two workers can get the same job.
4. **Run.** Your handler gets the job.
5. **Settle.** On success the job is removed from `reserved`. On failure with retries left it moves to `delayed`, scored with now plus the backoff. Out of retries, it's removed and written to `failed_jobs`.

Step 3 is why a crashed worker can't cause endless retries: the attempt is counted when the job is handed out, not when it finishes.

## The payload

```json
{
  "uuid": "0b1e7c6a-5f0e-4a8e-9d55-2c3f6b1d9a10",
  "displayName": "App\\Jobs\\SendWelcomeEmail",
  "job": "Illuminate\\Queue\\CallQueuedHandler@call",
  "maxTries": 3,
  "maxExceptions": null,
  "failOnTimeout": false,
  "backoff": "10,60",
  "timeout": 30,
  "retryUntil": null,
  "data": {
    "commandName": "App\\Jobs\\SendWelcomeEmail",
    "command": "O:25:\"App\\Jobs\\SendWelcomeEmail\":2:{s:6:\"userId\";i:42;s:5:\"email\";s:15:\"ada@example.com\";}"
  },
  "id": "Yx3mQ9pTqk0tGZ8cXw2nLr5vB7hJ1dSa",
  "attempts": 0
}
```

The outer document is the envelope: identity, retry policy and bookkeeping. `data.command` is the job object itself, run through PHP's `serialize()`. That inner string is what the [typed accessors](/docs/handlers/) read. Laravel 12 and 13 add a few more envelope keys, listed under [Laravel versions](/docs/laravel-versions/).

Notice that `$tries` and `$backoff` are in the envelope. Laravel copies them there at dispatch time, which is why a Go worker can honour them without knowing anything about the PHP class.

## Why sharing a queue is safe

Every state change above happens inside a Lua script. This package runs **the same scripts Laravel ships**. Redis runs a script atomically, so it makes no difference whether the worker that pops a job is PHP or Go. They take turns and each job goes to exactly one of them.

The scripts are identical in Laravel 10, 11, 12 and 13, which is also why supporting four major versions doesn't need four code paths.

## Two details worth knowing

**The reserved copy isn't byte-identical to what was pushed.** The pop script decodes the payload, increments `attempts` and re-encodes it inside Redis. Key order changes. An empty array comes back as an empty object. Nothing you need to handle (`job.Raw().Body` is the original, `job.Raw().Reserved` is Redis's copy), but it explains what you see if you compare the two by hand.

**At-least-once, not exactly-once.** If a worker finishes the work and dies before removing the job from `reserved`, the job runs again after `retry_after`. No queue built this way can avoid that. [Write handlers that tolerate a second run](/docs/going-to-production/#idempotent-handlers).
