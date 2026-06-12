# TD5 — Remove scale-out CI / test harness / build targets

**Parent:** [`dormant-scale-out-teardown.md`](dormant-scale-out-teardown.md) (§2 CI/scripts/build).
**Execution order:** **5th** (after TD4).
**Status:** Ready.
**Risk:** Low — deletes a workflow + host-side harness; no production runtime impact.

## Verified file inventory

| Path | Action | Verified anchor |
|---|---|---|
| [`.github/workflows/e2e-multiserver.yml`](../../.github/workflows/e2e-multiserver.yml) | **Delete** (workflow_dispatch + nightly cron `0 5 * * *`). | triggers confirmed |
| [`scripts/e2e-multiserver.sh`](../../scripts/e2e-multiserver.sh) | **Delete.** | exists |
| [`deploy/docker-compose.multiserver.yml`](../../deploy/docker-compose.multiserver.yml) | **Delete.** | exists |
| `server/tests/e2e-multiserver/` (`harness.go`, `load.go`, `main.go`, `scenario_reclaim.go`, `scenarios.go`) | **Delete** the whole directory. | dir exists, 5 files |
| [`Makefile`](../../Makefile) | Remove `e2e-multiserver` (`:257-258`) + `load-test-multiserver` (`:269-270`) recipes and both names from the `.PHONY` line (`:1`). | grep-confirmed |
| [`server/.gremlins.yaml`](../../server/.gremlins.yaml) | Remove the `tests/e2e-multiserver/` exclude (`:18`) + its comment (`:7-8`). | grep-confirmed |

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

- [ ] Workflow, script, compose, and `server/tests/e2e-multiserver/` all gone.
- [ ] `Makefile` has no `e2e-multiserver`/`load-test-multiserver` target or `.PHONY` entry.
- [ ] `.gremlins.yaml` has no e2e-multiserver exclude; gremlins config still valid.
- [ ] Documented false positives (docker-hub-mirror) left intact.
- [ ] `actionlint` + full `/precommit` gauntlet green.

## Done when

No CI workflow, Make target, compose file, or Go harness references the
multiserver path, and the build/lint/gauntlet are green.
