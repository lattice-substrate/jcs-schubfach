# Architecture

`jcs-schubfach` is split into small packages with one-way dependencies.

| Layer | Package | Responsibility | Must Not Depend On |
|------|---------|----------------|--------------------|
| L5 | `cmd/jcs-canon` | CLI argument handling, input selection, process exits | parsing internals other than exported APIs |
| L4 | `jcs` | Canonical serialization (`Value` -> canonical bytes) | CLI-specific code, OS-level side effects |
| L3 | `jcstoken` | Strict parser/tokenizer and profile checks (`bytes` -> `Value`) | CLI concerns, networking, subprocesses |
| L2 | `jcsfloat` | ECMA-262-compatible binary64 to string formatting (Schubfach algorithm) | CLI/runtime dependencies |
| L1 | `jcserr` | Stable error classes and exit code mapping | higher-level logic |

Dependency direction is inward only (L5 -> L1). Higher-level concerns cannot
contaminate lower-level guarantees, and each layer's correctness is provable
in isolation.

## Algorithm Difference from json-canon

This project is architecturally identical to [json-canon](https://github.com/lattice-substrate/json-canon)
except at L2 (number formatting):

- **json-canon**: Burger-Dybvig algorithm with `math/big.Int` multiprecision arithmetic
- **jcs-schubfach**: Schubfach algorithm with fixed-width 128-bit integer arithmetic

Both produce identical output for all finite IEEE 754 double-precision values.
The difference is in computational approach, not in behavior.

## Execution Flows

### Canonicalize Flow

1. Read bounded input bytes from stdin or file.
2. Validate UTF-8 and JSON grammar.
3. Enforce I-JSON and project profile restrictions.
4. Build internal typed value tree.
5. Serialize using canonical RFC 8785 rules.
6. Write canonical bytes to stdout.

### Verify Flow

1. Execute canonicalize flow in-memory.
2. Compare canonical output bytes with original input bytes.
3. Return success only on byte-identical equality.

## Trust Boundaries

Input from stdin/file is untrusted.

Mandatory boundary controls:

1. UTF-8 validation before semantic processing.
2. Grammar and profile checks before canonicalization.
3. Strict resource bounds on size, depth, and cardinality.
4. Stable classed errors for rejected input and internal faults.

## Determinism Model

Determinism is an architectural property, not a test-only property.

1. Output is a pure function of input bytes and options.
2. No wall-clock, RNG, locale, network, or subprocess dependence in runtime path.
3. Object member order is derived from UTF-16 code-unit sorting only.
4. Numeric emission follows the Schubfach algorithm using fixed-width 128-bit
   integer arithmetic. All operations are `math/bits.Mul64`, `math/bits.Add64`,
   and standard integer arithmetic -- none of which are susceptible to FMA fusion.

## Number Formatting Subsystem

`jcsfloat` uses the Schubfach algorithm with precomputed 128-bit powers of 10
and fixed-width 64x128-bit multiply for interval scaling. It is validated
against 286,000+ oracle vectors.

Invariants:

1. NaN and Infinity are rejected by profile.
2. `-0` is normalized to `0` at formatting level; lexical negative zero is rejected by parser policy.
3. Shortest round-tripping decimal representation is required.
4. Branch behavior around 1e-6 and 1e21 boundaries follows ECMA-262 rules.
5. No floating-point operations in the digit generation pipeline (FMA-immune).

## Failure Architecture

All externally visible failures are represented by stable classes
(`FAILURE_TAXONOMY.md`) and mapped to stable exit codes.

Architecture rules:

1. Classify by root cause, not by input source.
2. Preserve semantic class through wrapping/layer boundaries.
3. Keep machine-level semantics stable even if message text evolves.

## Compatibility Boundaries

The stable ABI boundary includes:

- CLI commands and flags,
- stream contracts (`stdout` vs `stderr`),
- exit code mapping and failure classes,
- canonical output bytes for accepted input.

Breaking this boundary requires a major version and migration documentation.
