# CLAUDE.md

Specifications and requirement registries govern product behavior.

## Hard Constraints

1. Linux only.
2. Static release binary (`CGO_ENABLED=0`).
3. No outbound network calls or subprocess execution in core runtime.
4. Canonicalization core does not use `encoding/json`.
5. Required conformance gates are Go-native (`go test`), not shell.
6. CLI ABI follows strict SemVer.
7. No tracked compiled binaries in repo root.

## Source-of-Truth Hierarchy

1. Normative standards text.
2. `REQ_REGISTRY_NORMATIVE.md`.
3. `REQ_ENFORCEMENT_MATRIX.md`.
4. `abi_manifest.json` and `FAILURE_TAXONOMY.md`.
5. This file, then supporting docs (`README.md`, `ARCHITECTURE.md`, `docs/*`).

## Validation Gates

Run before merge:

```bash
go vet ./...
go test ./... -count=1 -timeout=20m
go test ./... -race -count=1 -timeout=25m
```

For ABI-sensitive changes, also verify deterministic static build:

```bash
CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags="-s -w -buildid= -X main.version=v0.0.0-dev" \
  -o ./jcs-canon ./cmd/jcs-canon
```

## Change Workflow

For every non-trivial change:

1. Classify: normative, policy, ABI/CLI, internal refactor, or docs-only.
2. Identify impacted requirement IDs before editing.
3. Implement minimal change + regression tests.
4. Update traceability artifacts in the same change.
5. Run validation gates.
6. Update changelog and docs for user-visible changes.

## Engineering Invariants

1. Identical input + options produces identical bytes.
2. Classification by root cause, not input source.
3. Bounds enforcement is explicit, stable, and test-covered.
4. No dependence on map iteration order, time, locale, or environment.
5. No panic-based control flow in production paths.
6. Conformance claims require executable evidence.

## Prohibited

1. Silent ABI changes.
2. Behavior changes without test and traceability updates.
3. Undocumented conformance claims.
4. Network or subprocess execution in core packages.
5. `encoding/json` as canonicalization engine.
6. Weakening error-class contracts without a versioning decision.
7. Nondeterministic behavior in canonical paths.

## Definition of Done

1. Tests prove correctness at the right layer.
2. ABI impact handled (or explicitly none, with evidence).
3. Determinism and bounds guarantees intact.
4. Documentation ships with the behavior change.
