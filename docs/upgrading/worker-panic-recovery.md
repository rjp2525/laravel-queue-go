# Worker: panics in handlers are recovered

**Breaking:** no API changes. A panicking job no longer terminates the
process.

## What changed

A panic in a job handler or in a middleware used to crash the whole worker
process. With `Concurrency > 1` that also aborted every other job in flight
and the panicking job stayed in the reserved set to be picked up again after
`retry_after`, typically crashing the next worker as well.

The worker now recovers the panic and treats it as a failed attempt:

- the job is released for retry or failed according to `maxTries` /
  `retryUntil`, exactly as if the handler had returned an error.
- the error is a `*queue.PanicError` carrying the panic value and the stack.
- the stack is stored in the failed job's `exception` column.
- `errors.Is` / `errors.As` see through to the panic value when it is an
  `error`.

## What you need to do

- If you relied on a panic to restart the process (for example to let a
  supervisor recycle a worker in a bad state), do that explicitly now: listen
  for the failure and stop the worker.

  ```go
  mgr.On(events.JobExceptionOccurred, func(e events.Event) {
      var p *queue.PanicError
      if errors.As(e.Error, &p) {
          worker.Stop() // or os.Exit(1)
      }
  })
  ```
- Remove `defer recover()` boilerplate from handlers if its only purpose was
  to keep the worker alive.

Panics in event listeners are not recovered. Listeners run on the worker
goroutine and should not panic.
