---
number: 060
title: Hand-written msgpack encoder for ControlMessage
status: Accepted
date: 2026-07-26
---

# ADR-060: Hand-written msgpack encoder for ControlMessage

## Context

`ControlMessage` ([`control.go`](../../server/internal/protocol/control.go)) is a
union: one flat Go struct carrying the fields of every control-plane message
type, discriminated by `Type`. Rust models the same wire contract as a tagged
enum where each variant declares only its own fields, so the Go side is the only
one holding all 83 fields in a single shape. Every field but `Type` is
`omitempty`, and any given message populates a handful of them.

Encoding went through `msgpack.Marshal`, whose reflection-based struct encoder
decides which `omitempty` fields to skip by calling `reflect.Value.Interface()`
on each one and type-asserting the result to its internal `isZeroer`. That call
boxes the field value into an interface, which heap-allocates for every type that
is not pointer-shaped — strings, integers, floats. The assertion then fails for
essentially every field.

The cost is therefore **one heap allocation per declared `omitempty` field, on
every encode, regardless of message type**, and it grows with the struct. A
profile of `BenchmarkCodec_EncodeControl` attributed 93.6% of allocated objects
to `reflect.unsafe_New` under `(*fields).OmitEmpty` → `isEmptyValue`: 61
allocations per encode for a message that populates six fields. Encoding a
two-field heartbeat cost the same 79 allocations as a full register message.

The nightly benchmark trend measured the drift as the union grew:

| Date | allocs/op | B/op | ns/op |
|---|---|---|---|
| 2026-06-27 | 45 | 1176 | 2900 |
| 2026-07-02 | 57 | 1472 | 3417 |
| 2026-07-13 | 79 | 2048 | 4495 |
| 2026-07-26 | 80 | 2072 | 3825 |

That is +78% allocations and +76% bytes for an unchanged benchmark input, paid on
every control message the fleet sends — heartbeats and telemetry included. The
regression gate had been red every night since 2026-07-02 reporting exactly this.

Upstream offers no relief: `vmihailenco/msgpack/v5` v5.4.1 is the latest release
and still carries the `v.Interface()` call.

## Decision

`ControlMessage` implements `msgpack.CustomEncoder`
([`control_encode.go`](../../server/internal/protocol/control_encode.go)). The
encoder marks field presence into a stack-allocated `[83]bool` using direct typed
comparisons (`!= ""`, `!= 0`, `len(…) != 0`, `!= nil`), counts the marks for the
map header, and then writes only the marked fields. No reflection, no boxing.

The emitted bytes are **identical** to the reflection encoder's: the same field
order (declaration order), the same keys, the same `omitempty` semantics, and the
same width-preserving integer encoders the reflection path selects while compact
ints are off. Byte-identity — rather than merely "a valid encoding" — is the
decision, because it makes the change inert for every existing consumer: the
committed cross-language golden fixtures, the Rust decoder, and any agent already
deployed.

Decoding is untouched and stays reflective; `DecodeControl` allocates a
`ControlMessage` whose size is set by the struct, which this ADR does not change.

## Consequences

Measured on `BenchmarkCodec_EncodeControl`:

| | allocs/op | B/op | ns/op |
|---|---|---|---|
| Before | 80 | 2072 | 3879 |
| After | 4 | 264 | 569 |

Cost is now proportional to the fields a message populates rather than to the
number the union declares, so adding a wire field no longer makes every unrelated
message more expensive. The result is also well below the 2026-06-27 figures, so
this is not a restoration of a prior baseline but a step past it.

The trade is that the encoder must be kept in step with the struct by hand. Three
guards make that a test failure rather than a wire break, in
[`codec_wire_equivalence_test.go`](../../server/internal/protocol/codec_wire_equivalence_test.go):

- a per-field differential test that walks `ControlMessage` by reflection and, for
  every field, asserts the hand-written output is byte-identical to the reflection
  encoder's — so a dropped field, a wrong key, a reordering, or wrong `omitempty`
  semantics fails on the specific field;
- an all-fields-populated case pinning ordering and map length across the struct;
- allocation budgets asserting the cost does not scale with the field count.

A field added to the struct without a matching encoder entry fails the per-field
test immediately. The `controlFieldCount` constant is likewise reflection-checked
rather than trusted.

### Alternatives considered

- **Re-baseline the benchmark and accept the numbers.** Rejected: the gate was
  reporting a real, compounding cost on the hottest path in the product, and
  accepting it would have re-armed the gate against a worse baseline every time
  the protocol grew.
- **Split the union into per-type structs.** The correct end state and what Rust
  already does, but it changes every construction and consumption site across the
  server and is a protocol-wide refactor; it does not need to precede this fix and
  this fix does not foreclose it.
- **Make the fields pointer-shaped** so `reflect.Value.Interface()` stops
  allocating. Rejected: it fixes the symptom by making every field access in the
  codebase nil-checked, for a much worse API.
- **Drop `omitempty`.** Rejected: it inflates every message on the wire and breaks
  the field-presence contract the Rust decoder relies on.
