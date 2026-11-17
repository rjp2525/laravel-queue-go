# Go toolchain: CI on 1.26 and 1.27, modernized idioms

**Breaking:** no. The minimum Go version stays 1.26 (`go 1.26.1` in go.mod).

## What changed

- CI runs unit tests and builds on Go 1.26 (the minimum) and 1.27 (current),
  always with the latest patch release. Lint, vet and integration tests run
  on 1.27.
- Release binaries are built with the latest Go 1.27 patch release, so they
  include that toolchain's security fixes.
- A few loops and helpers use the standard library's newer forms
  (`maps.Clone`, `slices.Sorted(maps.Keys(...))`, `errors.AsType`,
  `for range n`). No behaviour change.
- `make vet` and `make fix` (the Go 1.26+ modernizers).

## Why the minimum is still 1.26

Go 1.27 adds things this module could use (a standard `uuid` package,
stable `encoding/json/v2`, generic methods), but importing any of them means
`go 1.27` in go.mod, which stops every user still on Go 1.26 from building.
Go 1.26 remains supported upstream until 1.28 is released, so the directive
is left alone. Nothing here stops you from building with 1.27: you get its
faster `encoding/json` (v1 is backed by the v2 implementation) and allocator
improvements without any change.

## What you need to do

Nothing. If you build with Go 1.27, use 1.27.4 or newer; the 1.27.2 to 1.27.4
releases contain security fixes in `net/http`, `crypto/tls`, `crypto/x509`
and the go command.
