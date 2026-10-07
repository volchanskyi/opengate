---
number: 20
title: Module boundaries in one deployable
---

# ADR-020 — Module boundaries in one deployable

## Context

The server, the agent and the web client each grew a layer that everything
reached into: one `db.Store` holding every query, one control-message handler
holding every branch, and a folder of stores any component could import. Each
made every change wider than it needed to be.

## Decision

**Boundaries are enforced inside one deployable.** Nothing is split into a
separate service to get a boundary; the boundary is the module, and the lint is
what holds it.

**A port is earned, not assumed.** An interface is added when two or more
callers need the same operation, when a test cannot isolate the unit without
one, or when the operation is a genuine candidate to move out of the process.
An interface with one caller and one implementation is deleted.

**Go — one repository per aggregate.** `audit`, `update`, `auth`, `device`,
`notifications`, `amt` and `session` each own their queries and their
read models. Transactions are owned by the use-case layer and threaded through
`context.Context`. No event bus, no separate read service, no distributed
transactions — consistency is one database and one transaction.

**Rust — a trait around the control fan-out.** `ControlMessageHandler` splits
the agent's inner dispatch by message family. The outer four-branch frame
dispatch stays plain code, because a multiplexer is not a policy.

**Web — one store per feature, reached through its index.** Each folder under
`src/features` is one feature and owns its state. A feature reaches its own files
freely and another feature only at that feature's `index.ts`, so what a feature
exports is a decision its index records. Shared components in `src/components`
use utilities, the global stores, each other and a feature's index, nothing
deeper. `useAuthStore` in `src/state` is the one global store, because everything
needs the session before anything else exists. The rules catalogue store is
shared through the rules index, since the investigations queue picks a rule from
the same catalogue the Rules list reads. The entry files (`main`, `App`, `router`)
are a file category that reaches everywhere.

**Three lints hold it**: `eslint-plugin-boundaries` and `dependency-cruiser` on
the web tree, `cargo-deny` on the agent, and `go-arch-lint` on the server. All
three fail the build rather than warn.

**A web boundary rule is proven to fire.** The boundary lint resolves
extensionless imports to their TypeScript files, since an import it cannot
resolve escapes every policy, and
[`eslint-boundaries.test.sh`](../../scripts/tests/eslint-boundaries.test.sh)
lints a probe import for each allowed and refused pair of layers — a feature's own
file, another feature's index and another feature's internals among them.
[`depcruise-check.sh`](../../scripts/depcruise-check.sh), run by the gauntlet
and CI alike, fails on any error the dependency check reports as well as on
warnings past its snapshot.

**What decides whether something leaves the process** is how intrusively it
couples, how many internal callers it has, and how often it changes. Something
leaves only when it is loosely coupled, has few callers, and there is a concrete
operational reason. `relay` stays — pairing is local. `db` is decoupled in place
and never split; it has the most callers of anything.

## Consequences

`internal/mps` sits inside the AMT module as its transport layer rather than
beside it. The `db.Store` interface is gone: once it held only `Ping` and
`Close` it stopped paying for itself.
