# ADR-016 — Bidirectional Golden Files + .meta.json Sidecars

**Status**: Accepted
**Date**: 2026-05-14
**Phase**: C1 of [Test Coverage Phase C: Structural Hardening](../../.claude/plans/archive/tests-coverage-phase-c-structural-hardening.md)
**Supersedes**: extends [ADR-002 — Golden file tests: Rust generates, Go verifies](Architecture-Decision-Records.md)

## Context

Until C1, golden file tests were one-directional. The Rust `mesh-protocol` crate generated `testdata/golden/*.bin` and the Go `server/internal/protocol` package verified them. A Go encoder change could silently break Rust deserialization without any CI signal. The gap was tracked in `.claude/techdebt.md` as "Golden File Tests Are One-Directional" since Phase 1.

A related concern: every golden was an unannotated `.bin`. There was no in-tree record of which protocol version produced it or which wire format the bytes use. A future protocol bump would force either (a) an all-or-nothing replacement of every fixture or (b) a side-channel convention for version-annotating filenames. Neither scales.

## Decision

Two coordinated additions:

### 1. Reverse goldens (Go → Rust)

Go encodes a representative subset of wire-protocol variants under `GENERATE_GOLDEN=1` and writes them to `testdata/golden/go_<variant>.bin`. The Rust crate has a new test file `agent/crates/mesh-protocol/tests/reverse_golden_test.rs` that reads each `go_*.bin`, decodes via the existing `Frame::decode` API, and asserts the canonical struct values.

The `make golden` target now runs both directions; the CI `golden` job sets up both toolchains (Go and Rust) and runs both verifications, then asserts `git diff --exit-code -- testdata/golden/` is clean.

### 2. `.meta.json` sidecars

For every `testdata/golden/*.bin`, a companion `<variant>.meta.json` describes its origin:

```json
{
  "variant": "control_agent_register",
  "protocol_version": 0,
  "format": "msgpack",
  "created": "2026-05-14"
}
```

`format` distinguishes msgpack control payloads (`msgpack`), the fixed binary handshake layout (`binary`), and the bare frame-type byte for ping/pong (`frame-only`). `protocol_version` is the wire-protocol version that produced the bytes — currently `0` for every fixture. The `TestGoldenSidecarsExist` Go test asserts every `.bin` has a valid sidecar, blocking commits that add `.bin` files without metadata.

Both Go and Rust generators write sidecars when `GENERATE_GOLDEN=1` is set:
- Rust forward generator emits a sidecar for each `.bin` it writes (existing `golden_check` helper, extended).
- Go reverse generator emits sidecars for the new `go_*.bin` files via `writeReverseGolden`.
- Go `TestGenerateForwardSidecars` writes sidecars for any forward `.bin` that lacks one — used during initial roll-in and remediation passes.

## Consequences

**Positive:**
- A Go encoder change that breaks Rust deserialization fails CI on the next push.
- The "Golden File Tests Are One-Directional" tech-debt item is closed.
- The sidecar convention makes future protocol-version bumps incremental: a v1 bump produces `v1_*.bin` and `v1_*.meta.json` files that coexist with the v0 fixtures, and both versions are verified.
- The Rust reverse verifier doubles as a smoke test for the Go encoder's output shape — useful when refactoring Go-side codec internals.

**Negative:**
- `testdata/golden/` doubles in file count (each `.bin` now has a `.meta.json`; plus 8 reverse `go_*.bin` and their sidecars). Storage cost is trivial (~5 KB total for the sidecars) but the directory listing is noisier.
- The CI `golden` job now sets up the Rust toolchain in addition to Go, adding ~30s of cold setup time.
- The reverse-golden coverage starts at 8 variants out of ~35 forward goldens. Extending coverage to remaining variants is a mechanical follow-up, but the gap is real.

## Alternatives Considered

- **Run Rust-side reverse verification inside the existing `rust-test` job** rather than in the `golden` job. Rejected because it would couple test cadences: rust-test fails would also fail the golden gate even when both are failing for unrelated reasons. The `golden` job's narrow scope (toolchains + drift check) is easier to debug when it goes red.
- **Add `protocol_version: u8` as a real wire field on `AgentProof`** as originally proposed in the C1 plan. Deferred. Adding the field today does not change any decoder behavior (no protocol bump is imminent), and a future protocol bump can introduce both the field and the v1 sidecars together. The sidecar convention is the load-bearing part of the design; the wire field is a follow-up when the first version bump lands.
- **Embed metadata inside the `.bin` files** (e.g. a fixed-length header). Rejected — would invalidate the fixtures as byte-exact reference values for the wire format. Sidecars keep the `.bin` files faithful to what goes over the wire.
