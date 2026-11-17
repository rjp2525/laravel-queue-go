# Dispatcher: chains use Laravel's format

**Breaking:** the contents of the `chained` property and therefore of
`job.ChainedJobs()`, for chains dispatched from Go.

## What was wrong

`DispatchOptions.Chain` stored each following job on the first job's `chained`
property as a complete JSON *queue payload* (`{"uuid":...,"job":...}`).
Laravel stores serialized *commands* there (`serialize($job)`) and continues
a chain with `unserialize(array_shift($this->chained))`. Given a JSON string,
`unserialize()` returns `false`, so a chain dispatched from Go ran its first
job and then failed in Laravel's worker when it tried to dispatch the second.

## What changed

Each `chained` entry is now the next job's command in the dispatcher's
format:

- `FormatPHP` (default): PHP `serialize()` output, `O:23:"App\Jobs\NextJob":1:{...}`.
- `FormatJSON`: the JSON command object, `{"commandName":"App\\Jobs\\NextJob",...}`.

## What you need to do

- **Laravel consumers:** nothing. Chains dispatched from Go now continue.
- **Go code reading `job.ChainedJobs()` of Go-dispatched chains:** each entry
  used to be a JSON queue payload. Decode it as a command instead.

  ```go
  // before
  var next queue.LaravelPayload
  json.Unmarshal([]byte(job.ChainedJobs()[0]), &next)
  name, command := next.Data.CommandName, next.Data.Command

  // after (FormatPHP)
  decoded, _ := phpserialize.Decode(job.ChainedJobs()[0])
  obj := decoded.(*phpserialize.Object)
  name, props := obj.ClassName, obj.Properties
  ```

  Entries of chains dispatched by Laravel were always serialized commands, so
  code that handled those already works this way.
- **Jobs already on the queue** with the old format keep their old `chained`
  contents. Laravel will fail them at the second step as before. Re-dispatch
  those chains.

Unchanged: a Go worker does not dispatch the next job of a chain. If a Go
handler processes a chained job, it must dispatch the next one itself.
