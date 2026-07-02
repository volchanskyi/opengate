# M0 — shared `scripts/lib/vm-query.sh` read-back helper

**Objective:** Generalize [`scripts/pmat-vm-query.sh`](../../../scripts/pmat-vm-query.sh)
into a reusable VM read-back library with **two modes** — `vm_query_latest` (newest
single sample, today's PMAT behavior) and `vm_query_window` (a window statistic via
`/api/v1/query` PromQL, returned **per-series keyed by labels** for the
multi-dimensional load-test case). Repoint PMAT at it, **behavior-preserving**.
Foundation for M1 (latest) and M2/M3 (window); **no behavior change to PMAT**.

**Dependencies:** none. **Blocks:** M2 (`vm-readback-m2-benchmark-nsop-gate.md`),
M3 (`vm-readback-m3-loadtest-gate.md`). M1 is complete
(`plans/archive/vm-readback-m1-mutation-drop-gate.md`).

## Context

`pmat-vm-query.sh` reads the **newest single sample** (`/api/v1/export` → `jq`
`max_by(timestamp)`) over a 30d window, via a `kubectl` curl-pod against the
in-cluster VM Service (`VM_NAMESPACE` / `VM_SERVICE` / `VM_CURL_IMAGE` — same
transport env as [`scripts/lib/vm-push.sh`](../../../scripts/lib/vm-push.sh)). That
is correct for **deterministic** metrics but wrong for **noisy** ones: M2/M3 need
an *aggregate over a window* (median), not a single prior point. Today's **fail-open
contract** — empty output + exit 0 on any transport/parse failure — must be preserved
(asserted by [`scripts/tests/pmat-vm-query.test.sh`](../../../scripts/tests/pmat-vm-query.test.sh)).

## Proposed surface — `scripts/lib/vm-query.sh` (sourced like `lib/vm-push.sh`)

- `vm_query_latest <metric> <selector>` — newest single sample via `/api/v1/export`
  (current PMAT path); **excludes the current commit**; prints a scalar or nothing.
- `vm_query_window <promql>` — scalar/vector from `/api/v1/query` for the window
  statistic (median / avg+σ / quantile); returns **per-series values keyed by labels**
  (so M3 can gate each `{scenario,phase,source}` independently); prints nothing on failure.
- Same `VM_NAMESPACE` / `VM_SERVICE` / `VM_CURL_IMAGE` env + `kubectl` curl-pod transport.
- **Fail-open contract:** any transport/parse failure ⇒ empty output, exit 0.

## File inventory

- **Create** `scripts/lib/vm-query.sh` — the two functions above, with a
  `[[ "${BASH_SOURCE[0]}" == "$0" ]]` direct-call guard (mirror `lib/vm-push.sh`).
  Current-commit exclusion parameterized (env/selector) so all consumers inherit it.
- **Modify** [`scripts/pmat-vm-query.sh`](../../../scripts/pmat-vm-query.sh) — become a
  thin adapter that sources `lib/vm-query.sh` and calls
  `vm_query_latest pmat_repo_score 'env="ci"'` / `pmat_below_bplus`. **Preserve** its
  CLI (`repo_score|below_bplus`), stdout, exit codes, and the `unknown field` failure.
- **Create** `scripts/tests/vm-query.test.sh` — mock `kubectl` on `PATH` (the
  [`pmat-vm-query.test.sh`](../../../scripts/tests/pmat-vm-query.test.sh) /
  [`vm-transport.test.sh`](../../../scripts/tests/vm-transport.test.sh) pattern):
  `vm_query_latest` returns the newest sample + fail-open (empty/invalid/transport-fail
  ⇒ empty, exit 0); `vm_query_window` parses a canned `/api/v1/query` response into
  per-series values + fail-open; current commit excluded from the export match.
- **Keep green** [`scripts/tests/pmat-vm-query.test.sh`](../../../scripts/tests/pmat-vm-query.test.sh)
  — the repoint is behavior-preserving; only adjust grep assertions if the query-arg
  *string shape* changes, never to weaken them.

## Steps (TDD-first)

1. **Test first:** write `vm-query.test.sh` (both modes + fail-open + current-commit
   exclusion) → red.
2. Extract `lib/vm-query.sh`: `vm_query_latest` ports pmat's `/api/v1/export` +
   `jq max_by(.timestamp)` logic verbatim; `vm_query_window` adds the `/api/v1/query`
   path + `jq` to a per-series label→value map.
3. Repoint `pmat-vm-query.sh` at `vm_query_latest`; keep `pmat-vm-query.test.sh` green.
4. `make shell-quality` green.

## Gotchas / constraints

- **Behavior-preserving for PMAT:** identical stdout / exit codes / `unknown field`
  handling; `pmat-vm-query.test.sh` must pass without weakening any assertion.
- **Fail-open everywhere** (empty on any failure, exit 0) — never exit non-zero on
  infra; this is PMAT's existing contract that M1/M2/M3 rely on.
- **Current-commit exclusion lives in the lib** (both modes) so consumers inherit it —
  a workflow re-run must not compare against its own just-pushed sample.
- `vm_query_window` returns **per-series keyed by labels**, not a single scalar — the
  M3 multi-dimensional case depends on this shape. M1 uses only `vm_query_latest`.
- **Read-only** — no VM *write* here (that stays in `lib/vm-push.sh`).

## Reviewer checklist

- [ ] Two functions with the documented surface; sourced-lib pattern like `lib/vm-push.sh`.
- [ ] PMAT repoint behavior-preserving: `pmat-vm-query.test.sh` green without weakening.
- [ ] Both modes fail-open (empty + exit 0) on transport/empty/invalid input.
- [ ] Current commit excluded in both modes.
- [ ] `vm_query_window` returns per-series values keyed by labels.
- [ ] `make shell-quality` + `/precommit` green.

## Verification

`make shell-test` (`vm-query.test.sh` + `pmat-vm-query.test.sh`); confirm
[`pmat-trend.yml`](../../../.github/workflows/pmat-trend.yml)'s two `pmat-vm-query.sh`
call sites still resolve. `/docs`: none (internal refactor) — a one-line
[`Monitoring.md`](../../../docs/Monitoring.md) note only if the shared lib is documented.
