# TD3 — Remove cross-server proxy + multiserver package + main.go wiring

**Parent:** [`dormant-scale-out-teardown.md`](dormant-scale-out-teardown.md) (§2 Server, §4, §5).
**Execution order:** **1st** (of TD3→TD2→TD1→TD4→TD5→TD6). Runs before TD2 because
`main.go`↔`backend.go` interlock (see Coordination).
**Status:** Ready (after teardown master is approved).
**Risk:** Medium — touches the server entrypoint and the API router, but removes
code that is **inert in production** (in-process registry ⇒ owner is always self,
so the `PeerDialer` is never consulted — readiness §3).

## Objective

Delete the cross-server relay proxy and all wiring that constructs/selects it, so
that after TD3 the relay is fed an **in-process registry and no `PeerDialer`**.
Leaves the `PeerDialer`/`proxied` *definitions* in `relay.go` dangling-but-unused
(TD1 removes those) and the redis backend *code* compiling-but-unselected (TD2
removes that).

## Verified file inventory

| Path | Action | Verified anchor |
|---|---|---|
| [`server/internal/api/internal_relay.go`](../../server/internal/api/internal_relay.go) | **Delete** (the `/internal/relay/{token}` route + `HTTPPeerDialer`). | file exists |
| [`server/internal/api/internal_relay_test.go`](../../server/internal/api/internal_relay_test.go) | **Delete**. | file exists |
| [`server/internal/multiserver/multiserver.go`](../../server/internal/multiserver/multiserver.go) | **Delete** the package (only file in it). | sole file |
| [`server/cmd/meshserver/main.go`](../../server/cmd/meshserver/main.go) | Remove `-internal-listen` flag (`:38`), `internalAddr` (`:160`), `proxySecret`/`OPENGATE_PROXY_SECRET` (`:162`), `peerDialer`/`api.NewHTTPPeerDialer` (`:163`), the internal listener `http.Server{Addr: internalAddr}` (`:245`), `OPENGATE_SERVER_ID`/`serverID` (`:347`), and the `REGISTRY_BACKEND` selection (`:397-402`). `buildRelayOptions` (`:380`) loses its `dialer relay.PeerDialer` param and the `relay.WithPeerDialer(dialer)` option (`:383`). Construct the registry as **in-process directly**. | grep-confirmed lines |
| [`server/cmd/meshserver/main_test.go`](../../server/cmd/meshserver/main_test.go) | Drop assertions covering the removed flags/env/listener; keep the rest green. | hit in sweep |

## Coordination (interlock with TD2)

`main.go:402` currently reads `REGISTRY_BACKEND` and routes through
`relay.SessionRegistryFromConfig` (backend.go), whose redis branch pulls in
`redis_registry.go` → `go-redis`. **Safe order:**

1. **TD3** rewires `main.go` to build an in-process registry **without** reading
   `REGISTRY_BACKEND` and without a `PeerDialer`. After this commit the redis
   branch in `backend.go` is *dead but still compiles* (nothing selects it) — the
   build and gauntlet stay green.
2. **TD2** then deletes `redis_registry.go`, collapses `backend.go` to
   inprocess-only, and drops the `go.mod` deps.

Do **not** remove `relay.WithPeerDialer`/`relay.PeerDialer` here — those live in
`relay.go` and are TD1's. After TD3 they simply have no caller (compiles fine).

## Steps (gauntlet green per commit)

1. **Test-first:** edit `main_test.go` + delete `internal_relay_test.go` (the test
   change that satisfies the TDD gate for this branch).
2. Delete `internal_relay.go`, `internal_relay_test.go`, `multiserver/multiserver.go`.
3. Edit `main.go`: drop the flag/env/listener/dialer/serverID/REGISTRY_BACKEND
   items above; construct the in-process registry directly; shrink
   `buildRelayOptions` to `(reg, logger)`.
4. `cd server && go build ./... && go vet ./...` — fix unused imports.
5. Grep guard: `grep -rnE 'NewHTTPPeerDialer|internal-listen|OPENGATE_PROXY|OPENGATE_SERVER_ID|REGISTRY_BACKEND|internal/multiserver' server/cmd server/internal/api` returns **zero**.
6. `/precommit` → commit → `/refactor` → push.

## Reviewer checklist

- [ ] `internal_relay.go(+test)` and `internal/multiserver/` are gone; nothing imports them.
- [ ] `main.go` builds an in-process registry with **no** `REGISTRY_BACKEND` read, **no** internal listener, **no** `PeerDialer`.
- [ ] `:9091` is no longer bound anywhere in the server entrypoint.
- [ ] `go build`/`go vet`/`make lint` clean; no unused imports.
- [ ] Backend redis code still compiles (it is TD2's to remove) — build green.
- [ ] Full `/precommit` gauntlet green.

## Done when

The server starts with an in-process registry and exposes no internal relay
route or listener; TD2 can proceed to delete the now-unselected redis backend.
