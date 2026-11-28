---
name: nightly-review
description: |
  Review every timed workflow run not yet written up, and every timed workflow whose run never
  came, before changing any file. Writes an outcome per item and records it, which lifts the hold
  the write guards and the commit guard place on the project.
---

# Nightly review

The rule is [`nightly-review.md`](../../rules/nightly-review.md). Until every item
`scripts/nightly-review.sh fetch` listed has a written outcome, no file in the project changes
and nothing is committed. Reading, searching and talking with Ivan stay open.

A run that reports a regression is a regression until shown otherwise. The review looks for
evidence that it is real, not for reasons that it is not.

## Steps

0. **The list** — `.claude/.markers/nightly.due`, written at session start. Run
   `scripts/nightly-review.sh fetch` when it is missing or says `unreachable`.
1. **Jobs and steps** — for each due run, `gh run view <id> --json jobs`: which jobs and steps
   ran, which were skipped, and why each skip fits the workflow's own conditions.
2. **What it left behind** — check the run against its row in
   [Scheduled runs](../../../docs/infrastructure/CI-Pipeline.md#scheduled-runs) and its workflow
   file: readings pushed, reports uploaded (`gh api repos/<owner>/<repo>/actions/runs/<id>/artifacts`),
   alerts sent.
3. **Failed steps** — read the log of every failed step, down to its error text
   (`gh api repos/<owner>/<repo>/actions/jobs/<job id>/logs`).
4. **A reported regression** — look for evidence that it is real:
   - the per-round readings;
   - the same job's earlier runs, newest first, until the reading changes;
   - what changed between the last good run and this one: commits, the runner's processor, the
     images and settings it ran against.
5. **Covered by a plan step** — show it is that same failure: the same step, the same error text.
6. **A workflow whose run never came** — say whether GitHub dropped it (the workflow is enabled,
   its other runs arrive) or the workflow is broken (disabled, or its file no longer parses).
7. **The outcome line** — one per item, in the write-up kept at
   `~/.cache/opengate/nightly-review/<date>.md`, outside the work tree:

   ```
   <run id | workflow file> <outcome>: <evidence>
   ```

   - **regression** — what broke, and the plan step or fast-lane commit that fixes it;
   - **broken-test** — the evidence that the test, not the product, is wrong;
   - **did-its-job** — the evidence that its work actually ran;
   - **not-found** — everything checked, and why the reported regression did not show. It goes to
     Ivan in a modal.
8. **A fix found** — joins the active plan after Ivan's yes in a modal, or takes the fast lane
   when production is down or deploys fail ([`plans-and-adrs.md`](../../rules/plans-and-adrs.md#plans)).
9. **Record it** — `scripts/nightly-review.sh done ~/.cache/opengate/nightly-review/<date>.md`.
   It refuses a write-up missing an item or giving one no known outcome, and on success empties
   the due list, which lifts the hold.
