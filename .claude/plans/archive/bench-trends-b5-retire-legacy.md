# Micro-Plan B5: Retire the gh-pages + Loki-Push Legacy Paths

**Parent master:** `benchmarks-grafana-trends.md` (§9 B5). **Branch:** `dev`.
**Owner:** CI/Bash. **Depends on (HARD GATE):** B2, B3, B4 each verified producing a
green VM trend. **Do not start the Loki removals until then.**

## 1. Goal

Remove the two superseded trend paths — the gh-pages benchmark path and the Loki-push
trend path — once their VM replacements are verified. Loki itself stays (logs).

## 2. Scope (two stages)

**Stage 1 — gh-pages benchmark path** (can land with/after B2):
- Remove `bench-publish` + the `go-bench`/`rust-bench` feeder jobs from
  [`ci.yml`](../../../.github/workflows/ci.yml).
- Fix `needs:` in `notify-failure` / `merge-to-main` so the graph stays valid.
- Drop the `github-action-benchmark` action pin; purge the gh-pages benchmark data
  branch/dir.

**Stage 2 — Loki-push trend path** (only after B3 verified):
- Delete `scripts/lib/loki-push.sh`, the mutation/PMAT/terraform-drift Loki
  wrappers, and `scripts/tests/loki-transport.test.sh`.
- Remove the old LogQL trend dashboards.

**Out:** removing Loki (stays for logs); touching runtime VM scraping.

## 3. Approach

1. **Confirm the gate:** B2/B3/B4 VM trends are green in Grafana (live). Attach evidence.
2. Stage 1: delete gh-pages jobs/pins; run `actionlint` + trace every `needs:` reference
   so no job points at a removed one.
3. Stage 2: delete Loki push scripts + transport + test + LogQL dashboards; grep the repo
   for any remaining reference to the deleted scripts/dashboards (workflows, Makefile,
   docs) and clean them.
4. `make shell-quality` (no dangling sourced files) + `/precommit` green.

## 4. Acceptance criteria / DoD

- [x] gh-pages benchmark path fully removed: no `bench-publish`/`go-bench`/`rust-bench`,
      no `github-action-benchmark` pin, gh-pages data purged.
- [x] `needs:` graph valid (`actionlint` clean); `notify-failure`/`merge-to-main` fixed.
- [x] Loki push scripts + transport + `loki-transport.test.sh` + old LogQL dashboards
      deleted; **no dangling references** anywhere (grep clean).
- [x] Loki still serves logs; runtime VM scraping untouched.
- [x] `/precommit` + `actionlint` green.

## 5. NFRs

- **Maintainability:** one trend store + transport remain; dead code/scripts gone.
- **Reliability:** hard gate prevents a trend-data gap (no deletion before VM is green).

## 6. Reviewer/QA checklist

- [x] Evidence that B2/B3/B4 VM trends were verified **before** the Loki deletions.
- [x] `grep -rn loki-push|github-action-benchmark|gh-pages` returns only intended
      removals (no orphan refs).
- [ ] CI graph still runs end-to-end (a dry-run/PR shows green).
- [x] Loki datasource + log pipelines untouched.
