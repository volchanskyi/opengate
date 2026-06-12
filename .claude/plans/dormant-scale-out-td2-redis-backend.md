# TD2 — Remove the Redis-backed SessionRegistry

**Parent:** [`dormant-scale-out-teardown.md`](dormant-scale-out-teardown.md) (§2 Server).
**Execution order:** **2nd** (after TD3, before TD1).
**Status:** Ready (after TD3 lands).
**Risk:** Low — the redis adapter **never ran on a live cluster** (readiness §3);
after TD3 nothing selects it.

## Objective

Delete the `RedisRegistry` adapter and all its tests, collapse
`SessionRegistryFromConfig` to inprocess-only, and drop the `go-redis` +
`miniredis` modules.

## Verified file inventory

| Path | Action | Verified anchor |
|---|---|---|
| `server/internal/relay/redis_registry.go` | **Delete**. | exists |
| `server/internal/relay/redis_registry_test.go` | **Delete**. | exists |
| `server/internal/relay/redis_registry_pubsub_test.go` | **Delete**. | exists |
| `server/internal/relay/redis_registry_semantics_test.go` | **Delete**. | exists |
| `server/internal/relay/backend.go` | Collapse `SessionRegistryFromConfig` to **inprocess-only**; remove `RedisConfig`/`RedisUniversalOptions`. | exists |
| `server/internal/relay/backend_test.go` | Drop the redis-branch cases; keep inprocess cases green. | exists |
| [`server/go.mod`](../../server/go.mod) | Drop `github.com/redis/go-redis/v9` (`:20`) and `github.com/alicebob/miniredis/v2` (`:9`) via `go mod tidy`. | grep-confirmed lines |
| `server/go.sum` | Shrinks via `go mod tidy`. | — |
| [`server/.gremlins.yaml`](../../server/.gremlins.yaml) | Remove any redis/miniredis carve-out. (Note: the only carve-out found is `tests/e2e-multiserver/` at `:18` — that belongs to **TD5**; verify whether any redis-specific entry remains here.) | `:7-8,:18` |

## Coordination

- **Requires TD3 first** (so `main.go` no longer selects redis; the branch is dead).
- `backend.go`'s `SessionRegistryFromConfig` may now be unused entirely if TD3
  built the registry directly. If so, either delete the function or keep a thin
  inprocess constructor — TD1 will confirm what the relay/`main.go` call. Keep
  whichever leaves `go build` green.

## Steps (gauntlet green per commit)

1. **Test-first:** delete the three `redis_registry_*_test.go` files and trim
   `backend_test.go` (satisfies the TDD gate as a test change on the branch).
2. Delete `redis_registry.go`; collapse `backend.go`.
3. `cd server && go mod tidy` — confirm `go.mod` no longer lists go-redis or
   miniredis and `go.sum` shrank.
4. `go build ./... && go vet ./... && make lint` clean (no unused imports/deps).
5. Grep guard: `grep -rnE 'go-redis|miniredis|RedisRegistry|RedisConfig|REDIS_' server/` returns **zero** (outside docs).
6. `/precommit` → commit → `/refactor` → push.

## Reviewer checklist

- [ ] All four redis adapter/test files gone; `backend.go` is inprocess-only.
- [ ] `go.mod`/`go.sum` no longer require go-redis or miniredis; `go mod tidy` is a no-op on re-run.
- [ ] No `REDIS_*` / `RedisRegistry` references remain in `server/`.
- [ ] `make lint` + full `/precommit` gauntlet green.

## Done when

The relay package compiles with a single in-process registry implementation and
the redis modules are gone from the build graph.
