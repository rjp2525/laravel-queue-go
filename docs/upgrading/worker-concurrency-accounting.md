# Worker: concurrency slots, MaxJobs and the stopping event

**Breaking:** no API changes. Two observable behaviour changes.

## What was wrong (Concurrency > 1)

- The worker reserved the next job *before* a goroutine was free to run it.
  With all slots busy, one extra job sat in `:reserved` with its `retry_after`
  clock running. If the running jobs took longer than `retry_after`, that job
  was migrated back and delivered to another worker and then also run here.
- `MaxJobs` was checked against *finished* jobs, so a worker with
  `Concurrency: 4, MaxJobs: 5` could process up to 9.
- `events.WorkerStopping` fired only when the context was already cancelled
  at the top of a fetch. It did not fire for `MaxJobs`, `MaxTime`,
  `StopOnEmpty` or a `Stop()` that arrived while the worker was waiting for a
  job.

## What changed

- A slot is taken before a job is reserved. At most `Concurrency` jobs are
  ever reserved by one worker.
- `MaxJobs` counts jobs handed to handlers. The worker never starts more.
- `WorkerStopping` fires exactly once per `Run`, for every stop reason, after
  in-flight jobs have drained and right before `Run` returns.

## What you need to do

- **`WorkerStopping` listeners:** the event used to fire (when it fired at
  all) before in-flight jobs finished. It now fires after they have. If you
  used it to start a shutdown timer for running jobs, call `worker.Stop()`
  yourself and start the timer there:

  ```go
  <-ctx.Done()          // your shutdown signal
  worker.Stop()         // stop taking jobs
  timer := time.AfterFunc(30*time.Second, hardCancel)
  ```
- **`MaxJobs` with `Concurrency > 1`:** the worker now stops at exactly
  `MaxJobs`. If you sized `MaxJobs` around the old overshoot, adjust it.
