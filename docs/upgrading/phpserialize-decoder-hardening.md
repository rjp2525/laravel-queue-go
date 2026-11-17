# phpserialize: decoder hardening

**Breaking:** no API changes. Behaviour changes only for payloads that use PHP
references or that are malformed.

## What changed

- `r:` / `R:` references now resolve to the value PHP would resolve them to.
  Previously array keys and property names were counted as reference slots,
  so a job with two properties pointing at the same object (or any `r:N;` /
  `R:N;` after the first property) decoded to the wrong value, or failed with
  `reference index out of range`.
- Malformed input returns an error instead of panicking or allocating
  unbounded memory: negative lengths (`s:-1:"";`), counts larger than the
  remaining input (`a:999999999999:{}`), `b:2;`, and unterminated `i:`/`d:`/`r:`
  values.
- Nesting is limited to `phpserialize.MaxDepth` (4096, PHP's default
  `unserialize_max_depth`).

## What you need to do

Nothing, unless you relied on the old reference behaviour:

- If a handler worked around wrong back-references (for example by reading a
  duplicated model from a different property), remove the workaround; both
  properties now hold the same `*phpserialize.Object`.
- Self-referencing objects now decode to a cyclic Go value. Code that walks
  `Object.Properties` recursively must guard against cycles.
- Strings that PHP's own `unserialize()` rejects are now rejected here too.
