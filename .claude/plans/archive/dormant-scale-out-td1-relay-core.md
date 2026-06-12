# TD1 — Relay core simplification (local-pairing only)

**Parent:** `dormant-scale-out-teardown.md` (§2 Server, §3, §4, §5).
**Execution order:** **3rd** (after TD3 + TD2, before TD4).
**Status:** Completed.
**Risk:** **Highest** — this is the **live production relay pairing path**. Hard
constraint: single-server pairing behavior is **byte-for-byte identical** before
and after; `relay_test.go` is green before and after.

## Outcome

- `relay.go`: `Register` collapsed to **local pairing only**. Removed `PeerDialer`,
  `WithPeerDialer`, `ErrSessionProxied`, the `proxied`/`done` session fields, the
  `peerDialer` field, `RegisterLocal`, `dropHalfOpen`, `resolveOwner`,
  `spliceToOwner`, `spliceProxied`, `affinityTTL`/`WithAffinityTTL`/
  `DefaultAffinityTTL`. `writeOwnerMeta` now only `SaveSession`s; `pipe` teardown
  only `DeleteSession`s.
- `registry.go`: `SessionRegistry` slimmed to **`SaveSession` / `DeleteSession` /
  `Ping`**. Dropped `ClaimAffinity`, `LookupOwner`, `SubscribeEvents`,
  `PublishEvent`, the `SessionEvent`/`EventKind` types, and the now-unused
  `ErrInvalidArgument` / `ErrRegistryNotFound`.
- `inprocess_registry.go`: dropped the four removed methods + the subscriber
  machinery; kept the `entries` map behind `SaveSession`/`DeleteSession`.
- Tests: deleted `relay_proxy_test.go`; trimmed `relay_test.go` (dropped
  affinity/event/foreign-owner and degraded-mode cases, **kept** all local-pairing
  and readiness cases) and rewrote `inprocess_registry_test.go` to the slimmed surface.
- `health.go` now contains only `PingRegistry`; the Redis-outage state machine,
  refusal branch, metric, monitor goroutine, and alert were removed.
- `cmd/meshserver/main.go` now constructs the default in-process relay directly.

## Objective

Collapse `relay.Register` to **local pairing only** and resolve the
**SessionRegistry port-depth decision (master §3)**: remove the distributed-only
methods, keeping the seam so a future rebuild is cheap.

## Verified file inventory

| Path | Action | Verified anchor |
|---|---|---|
| [`server/internal/relay/relay.go`](../../../server/internal/relay/relay.go) | Remove `PeerDialer` (`:67`), `WithPeerDialer` (`:162`), `ErrSessionProxied` (`:35`), the `proxied`/`done` session fields (`:80-81`), `peerDialer` field (`:139`), and the foreign-owner/proxied branch in `Register` (`:215-264`) + the proxy teardown helpers (`:340-378`) + the `ClaimAffinity` call (`:318`). Collapse to local pairing. | grep-confirmed lines |
| `server/internal/relay/relay_proxy_test.go` | **Delete** (tests the proxy path). | exists |
| [`server/internal/relay/relay_test.go`](../../../server/internal/relay/relay_test.go) | Drop proxy cases; **keep/strengthen** the local-pairing assertions (these are the behavior-preservation guard). | exists |
| [`server/internal/relay/registry.go`](../../../server/internal/relay/registry.go) | **Slim the `SessionRegistry` port** (master §3 option **B**): drop `ClaimAffinity`/`LookupOwner`/`SubscribeEvents`/`PublishEvent` (`:70,:74,:87,:91`) + the `SessionEvent` type if now unused. Keep the local-pairing essentials. | grep-confirmed methods |
| [`server/internal/relay/inprocess_registry.go`](../../../server/internal/relay/inprocess_registry.go) | Remove the now-removed methods' no-op impls; shrink to what local pairing uses. | exists |
| `server/internal/relay/inprocess_registry_test.go` | Adjust to the slimmed port. | exists |
| [`server/internal/relay/health.go`](../../../server/internal/relay/health.go) | Re-scope: the Redis-outage *degraded-mode* posture (ADR-023 C3) is moot single-server. Keep **only** what the single-server `/healthz`/readiness probe needs (master §5 risk 3). | exists |
| `server/internal/relay/backend.go` | If TD2 left a thin `SessionRegistryFromConfig`, confirm it returns the slimmed inprocess registry; otherwise delete if unused. | TD2 follow-up |

## Port-depth decision (finalize here)

Master §3 recommends **B — slim the port**. Verified: `ClaimAffinity` is called
only at `relay.go:318` (inside the proxy/affinity path being removed), and
`LookupOwner`/`SubscribeEvents`/`PublishEvent` have no caller once `Register`'s
foreign-owner branch is gone. **Action:** drop all four distributed methods; keep
the seam (the `SessionRegistry` interface name + local Register/Unregister/Lookup
shape) so a future Redis rebuild re-implements against it. Confirm the exact
local-essential method set by reading `relay.Register`'s post-collapse body before
cutting.

## Steps (gauntlet green per commit)

1. Run `cd server && go test ./internal/relay/...` — capture the **green
   local-pairing baseline**.
2. **Test-first:** trim `relay_test.go` (drop proxy cases, keep/strengthen local
   pairing) + delete `relay_proxy_test.go` + adjust `inprocess_registry_test.go`.
   This is the TDD-gate-satisfying test change **and** the behavior guard.
3. Collapse `relay.go` `Register` to local pairing; remove proxy fields/helpers.
4. Slim `registry.go` + `inprocess_registry.go`; re-scope `health.go`.
5. `go build ./... && go vet ./...`; re-run `go test ./internal/relay/...` — the
   kept local-pairing assertions must be **identically green**.
6. `/precommit` → commit → `/refactor` → push.

## Reviewer checklist

- [x] `relay_test.go` local-pairing assertions unchanged in meaning and green before/after.
- [x] No `proxied`/`PeerDialer`/`ErrSessionProxied`/`ClaimAffinity` symbols remain in `relay/`.
- [x] `SessionRegistry` keeps the seam but drops the 4 distributed methods; `InProcessRegistry` shrank accordingly.
- [x] `health.go` contains only the readiness seam used by the API health probe.
- [x] `go build`/`go vet`/`make lint` + full `/precommit` gauntlet green.

## Done when

The relay is local-pairing-only with a slimmed registry seam, production pairing
behavior provably unchanged, and no distributed/proxy symbols remain.
