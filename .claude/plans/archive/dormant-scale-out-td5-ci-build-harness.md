# TD5 — Remove scale-out CI / test harness / build targets

**Parent:** `dormant-scale-out-teardown.md` (§2 CI/scripts/build).
**Execution order:** **5th** (after TD4).
**Status:** Completed.
**Risk:** Low — deletes a workflow + host-side harness; no production runtime impact.

## Verified file inventory

| Path | Action | Verified anchor |
|---|---|---|
| `.github/workflows/e2e-multiserver.yml` | **Delete.** | removed |
| `scripts/e2e-multiserver.sh` | **Delete.** | removed |
| `deploy/docker-compose.multiserver.yml` | **Delete.** | removed |
| `server/tests/e2e-multiserver/` (`harness.go`, `load.go`, `main.go`, `scenario_degraded.go`, `scenario_reclaim.go`, `scenarios.go`) | **Delete** the whole directory. | removed |
| [`Makefile`](../../../Makefile) | Remove both targets and `.PHONY` entries. | completed |
| [`server/.gremlins.yaml`](../../../server/.gremlins.yaml) | Remove the harness carve-out and comment. | completed |

## Coordination

- **Requires TD2** (gremlins carve-out): if TD2 already touched `.gremlins.yaml`,
  reconcile — the only carve-out found is the `tests/e2e-multiserver/` exclude,
  which is **TD5's** to remove. Confirm no double-edit conflict.
- Independent of the relay/helm code; safe after TD4.

## Known false positives (do NOT delete — verify each)

- `scripts/tests/docker-hub-mirror.test.sh` and the docker-hub-mirror action
  mention an image only as a **cached-image example** — keep (master §2 note).
- Confirm via `git grep` context before cutting any sweep hit in `scripts/`.

## Steps (gauntlet green per commit)

1. **Test-first:** deleting `server/tests/e2e-multiserver/*.go` (test/harness
   code) is itself the test change satisfying the TDD gate on this branch.
2. Delete the workflow, script, compose file, and `server/tests/e2e-multiserver/`.
3. Edit `Makefile` (recipes + `.PHONY`) and `.gremlins.yaml`.
4. `cd server && go build ./... && go vet ./...` (the deleted harness was a
   separate `main` package — confirm nothing imported it).
5. `make lint` + `actionlint` clean; `make help`/dry-run shows no dangling target.
6. Grep guard: `grep -rnE 'e2e-multiserver|multiserver' .github Makefile scripts deploy server/tests` returns **zero**.
7. `/precommit` → commit → `/refactor` → push.

## Reviewer checklist

- [x] Workflow, script, compose, and `server/tests/e2e-multiserver/` all gone.
- [x] `Makefile` has no retired target or `.PHONY` entry.
- [x] `.gremlins.yaml` has no retired harness exclude; config remains valid.
- [x] Docker Hub mirror image examples remain intact; stale workflow detection removed.
- [x] `actionlint` + full `/precommit` gauntlet green.

## Done when

No CI workflow, Make target, compose file, or Go harness references the
multiserver path, and the build/lint/gauntlet are green.
