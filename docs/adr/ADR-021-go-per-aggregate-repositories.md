# ADR-021: Go per-aggregate repositories; transactions threaded via context.Context

Date: 2026-05-19
Status: Accepted

## Context

[ADR-020](ADR-020-modular-monolith-full-hexagonal.md) adopted full hexagonal architecture for the Go server, with twelve modules each owning named inbound and outbound ports. This ADR resolves how the monolithic `db.Store` interface ([`server/internal/db/store.go:16-98`](../../server/internal/db/store.go) — 56 methods across 13 entity groups, imported from 51 files) decomposes into per-aggregate repositories, and who owns transactions when a use case spans multiple aggregates.

The plan ([`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) §4.6, R2 Q2) resolved transaction ownership; this ADR codifies it.

## Decision

### Per-aggregate repositories

Each domain module owns its outbound `*Repository` port. The repositories are extracted from `db.Store` as triggers fire (plan §9). The current `db` package shrinks to the set of cross-cutting concerns that no domain owns (health, schema migrations, connection pool config). Target: **`db.Store` < 30 methods** after decomposition (down from 56).

Per-aggregate repositories at module landing:

| Repository | Module | Approx. methods |
|---|---|---|
| `DeviceRepository` | `device` | ~9 (Upsert/Get/List×3/Delete/UpdateGroup/SetStatus/ResetAll) |
| `GroupRepository` | `device` (groups belong to device aggregate) | 4 |
| `UserRepository` | `auth` | 5 |
| `SessionRepository` | `session` | 4 |
| `WebPushRepository` | `notification` | 4 |
| `AMTRepository` | `amt` | 4 |
| `EnrollmentTokenRepository` | `update` | 5 |
| `AuditStore` | `audit` | 2 |
| `DeviceHardwareRepository` | `device` | 2 |
| `DeviceUpdateRepository` | `update` | 3 |
| `SecurityGroupRepository` | `auth` (membership semantics) | 8 |

Each repository is an interface owned by its consuming domain module. The Postgres implementations live in the existing `db` package (or move to per-module `<module>/db/` packages — decided per extraction PR). The interface lives where it is consumed; the implementation lives where it is implemented.

### Transactions: use-case layer owns, threaded via context.Context

When a use case requires an atomic multi-aggregate write (e.g. delete a Device + cascade its `AgentSession`s + write an `AuditEvent`), the **use-case layer opens the transaction**, threads it through `context.Context`, and each repo call extracts the tx from context if present.

- Helper package: `server/internal/dbtx` exposing `Begin(ctx) (context.Context, Tx, error)`, `Commit(Tx) error`, `Rollback(Tx) error`. Tx wraps `pgx.Tx`; context carries it under an unexported key.
- Repository methods extract the tx via a helper: `q := dbtx.From(ctx, fallbackDB)`. The fallback is the bare connection pool for single-aggregate reads outside a transaction.
- Multi-aggregate writes live in the orchestrating `*Service` (inbound port), not in any single repo.

### Read-model patterns

For queries that span aggregates (e.g. "device with active sessions and hardware report"), each domain has a `query.go` sub-package owning dedicated view queries. These are NOT routed through the aggregate's primary repo. Pattern:

```
server/internal/device/
  service.go        // DeviceService inbound port + impl
  repository.go     // DeviceRepository outbound port
  query.go          // read-model queries: GetDeviceWithSessions, etc.
  db/postgres.go    // Postgres adapter for both
```

This avoids the N+1 fan-out trap when splitting the aggregate from the view.

### Migration order (opportunistic, per plan §9)

Per-aggregate repository extractions happen in this order as triggers fire:

1. `cert` — already isolated, lowest fan-in, safe pilot for the extraction template
2. `amt` — first functional AMT bug fix triggers
3. `notification` — already has `Notifier`; reaffirm the pattern
4. `update` — loose coupling, clean extraction candidate
5. `audit` — sink-only, low coordination cost
6. `auth` — cross-cutting but well-bounded
7. `device` + `session` — largest, paid down across multiple feature PRs

No greenfield refactor PR. Each extraction rides a real change.

## Out of scope

- **No event bus / domain events.** Multi-aggregate consistency stays transactional within a single Postgres database. A future ADR adds `EventPublisher` ports when a cross-service domain event first appears (plan §14).
- **No CQRS.** Read models live next to their write aggregate, not in a separate read service.
- **No two-phase commit / saga pattern.** Transactions are local to the single database.
- **No coordinator pattern for transactions.** The use-case-layer-owns-via-context approach is the single pattern.

## Consequences

**Positive.**

- Each domain owns its data shape. Code review can verify a use case touches only its aggregate's repository.
- The 56-method monolithic interface shrinks to <30. Each module's repository is small enough to understand at a glance.
- Future microservice extraction (plan §14) inherits cleanly: a module's repository travels with the service when extracted.

**Accepted trade-offs.**

- Transactions thread through context — slightly less explicit than a Tx parameter, slightly more idiomatic for Go.
- Multi-aggregate read fan-out risk on services that don't add a `query.go` view. Mitigated by code review against the per-domain pattern.
- The current `db` package's 51 import sites must drop opportunistically; some imports may linger for months until the relevant trigger fires. Accepted under the no-greenfield-refactor norm.

## References

- Plan: [`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) §4.1, §4.6, §6 (query sub-package pitfall), §7 (residual db.Store target)
- Upstream: [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) — modular-monolith scope and style
- Database: [ADR-014](ADR-014-postgres-migration.md) — Postgres via pgx/v5
- Pinch-point: [`server/internal/db/store.go:16-98`](../../server/internal/db/store.go)
