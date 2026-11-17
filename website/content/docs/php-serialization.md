---
title: PHP serialization
description: The phpserialize package, for when the typed accessors aren't enough.
---

Laravel stores a job as the output of PHP's `serialize()`. The `phpserialize` package reads and writes that format. The [job accessors](/docs/handlers/) are built on it, and you can use it directly when you need the raw structure.

```go
import "github.com/rjp2525/laravel-queue-go/pkg/phpserialize"
```

## Decoding

```go
value, err := phpserialize.Decode(`a:2:{s:4:"name";s:3:"Ada";s:3:"age";i:36;}`)
```

PHP values map to Go like this:

| PHP | Go |
|---|---|
| `null` | `nil` |
| `bool` | `bool` |
| `int` | `int64` |
| `float` | `float64` |
| `string` | `string` |
| `array` | `*phpserialize.Array` |
| object | `*phpserialize.Object` |
| enum case | `*phpserialize.Enum` |

### Arrays

A PHP array is an ordered map, and `Array` keeps it that way: parallel `Keys` and `Values` slices in PHP's order. Convert when you know the shape:

```go
arr := value.(*phpserialize.Array)

arr.Len()
arr.IsSequential() // keys are 0, 1, 2, ...
arr.ToSlice()      // []any, or nil if the array isn't a list
arr.ToMap()        // map[string]any; integer keys become "0", "1", ...
```

### Objects

```go
obj := value.(*phpserialize.Object)

obj.ClassName          // "App\Jobs\SendWelcomeEmail"
obj.Properties["email"] // any
```

Property names are stored without PHP's visibility markers, so a `protected $email` is just `"email"`. The helpers do the type assertions for you and return zero values on a miss:

```go
phpserialize.GetString(obj, "email")
phpserialize.GetInt(obj, "userId")
phpserialize.GetFloat(obj, "total")
phpserialize.GetBool(obj, "express")
phpserialize.GetSlice(obj, "tags")
phpserialize.GetMap(obj, "meta")
phpserialize.GetEnum(obj, "status")
```

Inside a handler, `job.Data()` is the decoded job object.

### References

When the same PHP object appears twice in a job, PHP writes it once and points to it the second time. Decoding preserves that: both properties hold the **same** `*Object`. An object that refers to itself decodes to a cyclic value, so guard against cycles if you walk the tree recursively.

### Models

```go
if ref := phpserialize.AsModelIdentifier(obj.Properties["user"]); ref != nil {
    fmt.Println(ref.Class, ref.ID)
}
```

## Encoding

```go
s, err := phpserialize.Encode(map[string]any{"name": "Ada", "tags": []string{"a", "b"}})
// a:2:{s:4:"name";s:3:"Ada";s:4:"tags";a:2:{i:0;s:1:"a";i:1;s:1:"b";}}

cmd, err := phpserialize.MarshalObject(`App\Jobs\SendInvoice`, map[string]any{"invoiceId": 9001})
// O:20:"App\Jobs\SendInvoice":1:{s:9:"invoiceId";i:9001;}
```

Go integers, floats, strings, bools, `nil`, slices and maps are supported, along with `*Object`, `*Array` and `*Enum`. Maps are written with their keys sorted, so output is stable.

To send an enum to PHP:

```go
&phpserialize.Enum{ClassName: `App\Enums\OrderStatus`, CaseName: "Shipped"}
```

## Safety

Payloads come out of Redis, and Redis is not always written to by code you trust. The decoder is built for that:

- Malformed input returns an error. It never panics.
- Lengths and counts are checked against the input, so a forged `a:999999999:{` can't make it allocate.
- Nesting is capped at `phpserialize.MaxDepth` (4096, PHP's own default).

Decoding only builds data. Unlike `unserialize()` in PHP, it never instantiates classes or runs code, so there are no object-injection gadgets to worry about.

## What it can't read

- **`Serializable` objects** (the old `C:` format) decode to an `Object` with the right class name and no properties. Their contents are opaque.
- **Closures.** A queued closure is a signed, serialized PHP function. There is nothing to run in Go.
- **Encrypted commands.** Jobs that implement `ShouldBeEncrypted` carry ciphertext, not a serialized object.
