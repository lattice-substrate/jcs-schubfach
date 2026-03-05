# Resource Bounds and Memory Behavior

## Default Bounds

| Bound | Default | Constant |
|-------|---------|----------|
| Max nesting depth | 1,000 | `DefaultMaxDepth` |
| Max input size | 64 MiB | `DefaultMaxInputSize` |
| Max JSON values | 1,000,000 | `DefaultMaxValues` |
| Max object members | 250,000 per object | `DefaultMaxObjectMembers` |
| Max array elements | 250,000 per array | `DefaultMaxArrayElements` |
| Max string bytes | 8 MiB (decoded UTF-8 bytes) | `DefaultMaxStringBytes` |
| Max number chars | 4,096 | `DefaultMaxNumberChars` |

Library callers can configure bounds through `jcstoken.Options` on:
- `jcstoken.ParseWithOptions`
- `jcs.CanonicalizeWithOptions`
- `jcs.SerializeWithOptions`

`jcs.Serialize` and the CLI use default bounds.

Bound violations produce `BOUND_EXCEEDED` (exit code 2) with a diagnostic
indicating which bound was exceeded.

## Memory Behavior

### Parse Phase

The parser (`jcstoken.Parse`) builds a complete in-memory tree (`jcstoken.Value`).
Memory consumption is proportional to input size and structure complexity.

### Serialize Phase

`jcs.Serialize` writes to an in-memory byte buffer. Canonical output is
deterministic, but it is not always smaller than input bytes. Number
normalization can expand compact exponent forms (example: `1e20` -> `100000000000000000000`).

### CLI Behavior

The CLI reads the entire input into memory before parsing (`readBounded`).
Peak memory during canonicalization is approximately:

```
input_bytes + parsed_tree + canonical_output
```

For mixed payloads, ~3x input size is typical. With the default 64 MiB input
bound, budgeting 256-384 MiB process memory is a safer operational baseline.

## Nondeterminism Sources

The parser and serializer contain no nondeterminism sources:
- No `math/rand` or `crypto/rand` imports.
- No `time`-dependent behavior.
- No map iteration in output-affecting paths.
- Object key ordering uses deterministic UTF-16 code-unit comparison.
- Number formatting uses fixed-width integer arithmetic (Schubfach algorithm)
  with no floating-point operations in the digit generation pipeline.
