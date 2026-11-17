# v2: the module path ends in /v2

**Breaking:** yes. Every import path changes.

## What changed

The module is now `github.com/rjp2525/laravel-queue-go/v2`. Go requires the
major version in the module path from v2 on, which is what lets v1 and v2 be
installed side by side.

The `v2.0.0` tag was published before this change. Go rejects it
(`module path must match major version`), so it cannot be installed. The
first installable v2 release is **v2.0.1**.

## What you need to do

1. Get the new module:

   ```bash
   go get github.com/rjp2525/laravel-queue-go/v2
   ```

2. Update imports. Every `laravel-queue-go/pkg/...` becomes
   `laravel-queue-go/v2/pkg/...`:

   ```bash
   # GNU sed; on macOS use: sed -i '' ...
   grep -rl 'rjp2525/laravel-queue-go/pkg/' --include='*.go' . \
     | xargs sed -i 's#rjp2525/laravel-queue-go/pkg/#rjp2525/laravel-queue-go/v2/pkg/#g'
   ```

3. Drop the old requirement:

   ```bash
   go mod tidy
   ```

4. If you install the binary:

   ```bash
   go install github.com/rjp2525/laravel-queue-go/v2/cmd/lqworker@latest
   ```

Then work through the behaviour changes listed in the other guides in this
directory or the summary at
https://laravel-queue-go.reno.sh/docs/upgrading/.

v1 stays available at the old path (`go get github.com/rjp2525/laravel-queue-go@v1.3.0`)
and receives no further updates.
