# jcs-schubfach

RFC 8785 JSON Canonicalization Scheme (JCS) implementation in Go, using the
Schubfach algorithm for ECMA-262 number formatting.

This project is architecturally identical to
[json-canon](https://github.com/lattice-substrate/json-canon) -- the same
strict parser, the same UTF-16 key sorting, the same failure taxonomy, the same
conformance vectors. The single difference is the number formatting algorithm:
json-canon uses Burger-Dybvig with `math/big.Int` multiprecision arithmetic;
jcs-schubfach uses the Schubfach algorithm with fixed-width 128-bit integer
arithmetic.

Both produce identical output for all finite IEEE 754 double-precision values,
validated against 286,362 oracle test vectors.

## Why Two Implementations

The two projects exist to enable a rigorous technical comparison between
fundamentally different approaches to the same problem:

- **Burger-Dybvig** (json-canon): correctness is constructive. Each step
  maintains an exact invariant over rational intervals using multiprecision
  integers. The proof lives in the code.

- **Schubfach** (this project): correctness is analytic. It works because error
  bounds on fixed-width truncation are provably smaller than the gap between
  adjacent representable decimals. The proof lives in the paper, not the code.

## Performance

Schubfach is 3-12x faster than Burger-Dybvig across all input classes, with
lower allocation counts and no GC pressure from `math/big.Int`.

## Architecture

| Layer | Package | Responsibility |
|-------|---------|----------------|
| L5 | `cmd/jcs-canon` | CLI tool |
| L4 | `jcs` | RFC 8785 canonical serializer |
| L3 | `jcstoken` | Strict RFC 8259 parser |
| L2 | `jcsfloat` | ECMA-262 number formatting (Schubfach) |
| L1 | `jcserr` | Error taxonomy |

## Usage

### Library

```go
import "github.com/lattice-substrate/jcs-schubfach/jcs"

canonical, err := jcs.Canonicalize(input)
```

### CLI

```bash
# Canonicalize
jcs-canon canonicalize input.json

# Verify
jcs-canon verify input.json
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Input rejection (parse error, non-canonical, CLI usage) |
| 10 | Internal error (I/O failure) |

## Testing

```bash
go test ./... -count=1
go test ./... -race -count=1
```

## Normative References

- [RFC 8785](https://www.rfc-editor.org/rfc/rfc8785) -- JSON Canonicalization Scheme
- [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259) -- The JavaScript Object Notation (JSON) Data Interchange Format
- [RFC 7493](https://www.rfc-editor.org/rfc/rfc7493) -- The I-JSON Message Format
- [ECMA-262](https://tc39.es/ecma262/) -- ECMAScript Language Specification
- [IEEE 754-2019](https://standards.ieee.org/ieee/754/6210/) -- Floating-Point Arithmetic

## License

Apache License 2.0
