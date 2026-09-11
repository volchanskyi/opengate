# Mutation testing: split the legs, narrow the coverage survey

**Status:** draft, for iteration.
**Owner:** Ivan.
**Related:** [`.claude/techdebt.md`](../techdebt.md) — "The mutation score has not
recovered from the rules and alerts surface" (this plan touches the same score).

## Why

Twelve of the last twenty scheduled nightly runs produced no score at all. The
cause is not the mutation tooling; it is two structural choices that turn a
small, ordinary defect into a total loss.

### One: every Go shard surveys the whole module

`gremlins unleash .` does two separate things, and the workflow narrows only one:

| Step | Scope today | Narrowed per shard |
|---|---|---|
| Coverage survey — which lines count as reachable | whole module, 41 packages | no |
| Kill runs — does breaking this line fail a test | the mutated package's tests only | n/a |

The kill step is package-scoped because `-i` / `--integration` is not passed
(`gremlins unleash --help`: *"makes Gremlins run the complete test suite for
each mutation"*). That is why a Go mutant costs 40–70 s rather than the 200–300 s
a module-wide suite run takes.

So `tests/acceptance`, `tests/integration`, `tests/loadtest`, `tests/netfault`
and `tests/vmramseries` run **once per shard, in the survey only**. Twenty-seven
Go shards, so the full server suite runs twenty-seven times a night, from cold
(`GOFLAGS=-count=1`). One flaky test anywhere in it is twenty-seven independent
draws.

Measured on 2026-09-06, `server/` at `5cc93aa3`:

| Coverage of `internal/` | |
|---|---|
| survey over `./internal/...` | 82.7% |
| survey over `./...` (today) | 83.3% |

**71 statements of 12 104 — 0.59%.** Nine are in `openapi_gen.go`, which is
excluded from mutation, leaving ~62 statements of mutable code across 25 files.
That is the entire return on the wider survey. Against it: ~70 CPU-minutes a
night (156 s × 27), and every baseline-flake failure in the last two weeks.

Those 71 statements also cost score rather than earning it. A line reached only
by an acceptance test is marked **covered**, so gremlins mutates it — then tests
it with the package's own unit tests, which do not reach it, so the mutant
**survives**. They are structurally unkillable under the current invocation and
are read as missing assertions. Some share of the 96 LIVED mutants in the
techdebt entry is this.

Per-package survey cost, from a nightly shard log:

| | seconds |
|---|---|
| `internal/*` | 640 |
| `tests/*` harnesses | 156 |
| total package time | 796 |

### Two: one lost shard voids all fifty-three

`publish` has `needs: mutation` over the whole matrix and a single `complete`
boolean. One shard short and no language gets a row.

Failing leg, last ten red nights:

| Failing leg | Nights |
|---|---|
| Go only | 6 |
| Go + Rust | 2 |
| Rust only | 1 |
| Neither — artifact lost after a green upload | 1 |

Web has not failed a shard in this window. On six nights the 25 Rust shards and
the web shard all finished and their scores were discarded.

This is also a **detection gap**, not only waste: a Rust score regression is
invisible whenever the Go leg fails, because `publish` never runs.

## What this plan does not do

- It does not change what gets mutated. The shard map, per-shard excludes and
  the budget projection are untouched.
- It does not relax any gate. A leg that does not complete still fails, still
  produces no row for that leg, and still goes red.
- It does not add a retry, a rerun, or a quarantine anywhere.

## Design decisions to settle before implementation

These are the parts worth arguing about. Each has a recommendation; none is
implemented until agreed.

### D1 — How to narrow the survey

`gremlins unleash` takes `--coverpkg`, a comma-separated package pattern list,
mirroring `go test -coverpkg`.

- **Option A: keep the survey module-wide, narrow the measured packages.**
  `--coverpkg ./internal/...`. Harness tests still *run*; only `internal/` lines
  are surveyed. It is the smaller diff, and it does fix the score distortion —
  but the harness tests still execute in every shard, so it keeps the
  ~70 CPU-minutes and, decisively, keeps the flake exposure that is the whole
  reason for this plan. **Rejected**, recorded so the option is on the page.
- **Option B (recommended): survey `./internal/...` only.** Point gremlins at a
  narrower path so the harness packages do not run in the survey at all.
  Removes the flake exposure and the CPU-minutes, and removes the 71
  structurally-unkillable statements from the mutant set.
- **Option C: keep the wide survey, mark harness-only lines as carve-outs.**
  Preserves the score's current meaning while making the unkillable set
  explicit. More machinery, same fragility.

**Open question:** does `gremlins unleash ./internal/...` accept a package
pattern, or only a single directory path? The help text says
`unleash [path]`. **This must be verified empirically before anything else in
this plan is written** — the whole narrowing half depends on it. If gremlins
takes only a path, the fallback is `--coverpkg` plus a build-tag or a
`-E`-style mechanism to keep harness packages out of the survey run, and D1 is
re-opened.

### D2 — What happens to the 71 statements

They stop being counted as covered, so they move from LIVED (survivor) to
absent. That **raises** the Go score by removing guaranteed survivors.

That is a score discontinuity, and `mutation-summarize.sh` compares each run
against the previous one with an absolute floor of 85.0. A one-night jump
upward is not a regression, so nothing goes red — but the baseline shifts, and
the techdebt entry's "88.2 target" is stated against the old meaning.

**Recommendation:** measure the new Go score on a `workflow_dispatch` run
before merging, and restate the techdebt entry's numbers against the new
meaning in the same commit that lands the change. Do not silently let the
target drift.

**Open question:** do we want those 62 statements tested at all? If yes, the
honest fix is a unit test that reaches them, not a wider survey. Worth listing
the 25 files and deciding per file — that is a separate workstream, and this
plan should name it rather than absorb it.

### D3 — How far to separate the legs

The thing that destroys the scores is `publish`'s single `complete` boolean, not
the job graph. That reframes the options.

- **Option A: three workflow files.** Each owns its matrix, publish and gate.
  Buys independent red/green and independent schedules. Forces every sweep that
  globs `.github/workflows/*` to move with it — including the `gh api` and
  `cargo install --locked` sweeps in
  [`ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh),
  which **fail when they match nothing**. Large blast radius.
- **Option B: one workflow, three matrix jobs and three publish jobs.** No
  filename churn. Still restructures the job graph, still triples the subset
  logic. GitHub cannot `needs:` part of a matrix, so this needs three matrices.
- **Option C (recommended): one workflow, one matrix, one publish that handles a
  partial set.** `publish` already waits for everything, which costs nothing.
  Teach it to emit a row for each leg that completed and to fail only when none
  did. Delivers the entire stated benefit — Rust and web keep their scores and
  their regression checks when Go flakes — for three conditional edits and two
  script changes.

**Recommendation: Option C**, on minimality. A and B both restructure the job
graph to fix something the job graph is not causing. C leaves a clean path to A
if independent scheduling is ever wanted.

**Resolved by choosing C:** the `observability` environment question disappears
— there is still exactly one consumer of it, so no serialisation and no
concurrency limit to check.

### D4 — Score row and baseline

The data model is **already per-language**, which makes this much cheaper than
it looks:

- `mutation-vm-push.sh` emits `mutation_score{language="go"}` etc., iterating
  `.scores | to_entries`.
- `mutation-baseline-fetch.sh` already queries per language and **omits a
  language with no history**.
- `regression_check` already treats a null language as floor-only.
- `mutation-status-build.sh` already tracks `rust_valid` and `go_valid`
  separately — it simply does not emit them.

What actually blocks a per-leg publish:

1. `mutation-status-build.sh` emits one `complete` boolean.
2. `mutation-summarize.sh`'s `build_row` requires all three inputs
   (`parse_rust`/`parse_go`/`parse_web`, each `|| return 2`).
3. `publish` has `needs: mutation` over the whole matrix.

**Recommendation:** make the summarizer accept a language subset rather than
splitting it into three scripts. One script, one canonical row shape, a row
that carries only the leg it ran for. The VM series are already keyed by
language, so three partial rows land exactly where one whole row does today.

**Open question:** the canonical row artifact is named `mutation-canonical-row`
and there is one per run. Three legs means three artifacts — decide whether
they stay separate (`mutation-canonical-row-go`) or an aggregator recombines
them. Recommendation: keep them separate; nothing downstream reads a combined
row that VictoriaMetrics does not already serve per language.

## Landing discipline

**Everything lands in one commit.** Both changes, their guards, the docs, the
ADR, the state rows and the retirement of this plan are a single
`/precommit` → `git commit` → `/refactor` → `git push origin dev`.

That constraint decides where the evidence comes from. Today's numbers for
per-mutant cost and the leash were read off completed **nightly** runs, and the
obvious sequence — change it, push, watch a night, correct the numbers — is two
commits. So every number this change depends on is measured **locally, before
the commit exists**, and the phase below that does it produces no push at all.

Two things follow:

- **No step may be "land it and see".** If a question can only be answered by a
  CI run, it is answered by a local shard run first, and the CI run afterwards
  is confirmation, not discovery.
- **The post-push phase is a watch with a rollback trigger, not a fix-up.** If
  the first night contradicts the local measurements, the commit is reverted
  whole and this plan is re-opened. It is not patched forward.

### What one commit costs, stated plainly

The two halves are independent changes landing together, so a bad night cannot
be attributed to one of them without reading the run. Two things keep that
manageable, and both are conditions on how the commit is built:

- **The halves touch disjoint files.** Narrowing touches the gremlins
  invocation and `mutation-shards.sh`; separating the legs touches the two
  summarizer scripts and the publish job's conditionals. A partial revert stays
  mechanically possible even though the commit is one.
- **The run status tells them apart.** A narrowing fault shows as a changed
  score, timed-out count, or shard wall clock. A leg-separation fault shows as a
  missing or duplicated per-language row. Neither presents as the other.

If either condition breaks while building the commit — a file needed by both
halves, or a failure mode that reads the same either way — stop and raise
splitting this into two commits before continuing.

## Minimality — what this change is *not*

The goal is: **a flake in one leg must not destroy the other legs' scores, and
the coverage survey must stop dragging the harness packages through every Go
shard.** Everything not required for that is out.

The largest thing dropped is the job graph. "Split the workflows" was the
framing, but the job graph is not what loses the scores — the **publish job's
single `complete` boolean** is. `publish` already waits for the whole matrix and
that is harmless; what hurts is that it refuses to emit *any* row when *any*
shard is missing.

So the minimal form keeps one workflow, one matrix, one publish job, one gate,
and changes what publish does with a partial artifact set. That delivers the
whole stated benefit — Rust and web keep their scores and their regression
checks when Go flakes — while touching no `needs:`, no matrix, no services, and
no workflow filename.

Explicitly **not** in this change, and why:

| Dropped | Why it is not needed for the goal |
|---|---|
| Three matrix jobs | The matrix is not what voids the scores. Splitting it changes scheduling and job-graph shape, neither of which is the problem. |
| Three publish jobs / three gates | One publish that handles a subset does the same work. Three of them is three places for the subset logic to drift. |
| Three workflow *files* | Would force every sweep that globs `.github/workflows/*` to move with it, including the `gh api` and `cargo install --locked` sweeps that fail when they match nothing. Large blast radius, no additional benefit to the goal. |
| Moving Postgres / VictoriaMetrics services to a Go-only matrix | A pure tidy-up. Costs nothing today; saves nothing that matters. |
| A reusable `workflow_call` shard body | Only earns its keep once there are separate files to share it between. |
| Unit tests for the ~62 newly-unsurveyed statements | Real work, genuinely worth doing, and entirely separable. Named in D2 as its own workstream. |

If independent red/green per leg or independent schedules turn out to be wanted
later, the job-graph split is a clean follow-on from this state and nothing here
blocks it.

## Phase 1 — Evidence (local only, no commit, no push)

Nothing here is committed. It ends by writing its measurements into this plan,
which is then carried into the single commit alongside the code.

1. **Verify the invocation.** Confirm `gremlins unleash` accepts a package
   pattern and that `--coverpkg` behaves as documented. If D1 Option B is not
   possible, **stop** — re-open D1 before anything else.
2. **Measure one cheap Go shard both ways.** Pick a shard with no
   Postgres-backed mutants. Record, for the wide and the narrow survey: survey
   elapsed, mutants total / covered / killed / lived / not-covered / timed-out,
   and wall clock.
3. **Measure one Postgres-backed Go shard both ways.** This is the one that
   matters, because it carries the slow mutants the leash can cut off. Record
   the same figures plus the slowest **finishing** mutant's duration.
4. **Check the leash against that number.** The leash is
   `timeout-coefficient` × survey elapsed, and the survey gets shorter here by
   design. Confirm the new leash is still comfortably above the slowest
   finishing mutant. If it is not, the coefficient moves **in this same commit**
   — it is not a follow-up.
5. **Re-derive `mutation_go_shard_seconds_per_mutant`** if step 2 or 3 shows the
   per-mutant cost moved. Measure over the mutants that **finish** — not
   `elapsed / mutants_total`, which charges every mutant for one blocked
   mutant's whole leash. Expectation: unchanged, because kill runs are already
   package-scoped and the survey narrowing does not touch them. Record the
   measurement either way, including "unchanged".
6. **Record the Go score delta** the narrowing produces on the shards measured,
   and reconcile it against the ~62 mutable statements that stop being surveyed.
   The score should **rise**, because those lines are guaranteed survivors today.
7. **Exercise the summarizer changes locally** against hand-built fixtures: one
   leg, two legs, a leg present-but-malformed.

**Exit condition:** every number the commit depends on is written into this plan
with the command that produced it. Nothing below is written until this is done.

## Phase 2 — The commit

One commit, in this order of construction so the guards exist before the things
they guard.

### Narrowing the survey

1. Change the gremlins invocation in
   [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml) and
   in the `mutate-go` target of the [`Makefile`](../../Makefile) **together** —
   they must not drift, and `scripts/tests/mutation-workflow.test.sh` gets a
   case holding them equal.
2. Apply the coefficient and per-mutant costs from Phase 1, steps 4–5. If both
   were unchanged, say so in the commit body rather than leaving it silent.
3. Update the comment in
   [`scripts/lib/mutation-shards.sh`](../../scripts/lib/mutation-shards.sh)
   ("Every Go shard runs `gremlins unleash .` so the coverage dry-run remains
   module-wide") — that sentence becomes false, and it is the single place the
   old reasoning is written down.

### Separating the legs

The job graph is untouched. Only what `publish` does with a partial set changes.

4. `scripts/mutation-status-build.sh`: emit `complete_by_language` alongside the
   existing `complete`. `rust_valid` and `go_valid` already exist internally and
   are simply not emitted; add the web equivalent. `complete` keeps its current
   meaning, so anything still reading it is unaffected.
5. `scripts/mutation-summarize.sh`: accept a language subset. A row carries only
   the legs it was given; a leg not asked for is absent rather than an error.
   Preserve exit 2 for *malformed* input — the distinction between "not asked
   for" and "asked for and broken" is the one that must not blur.
6. In [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml)'s
   `publish` job, three edits and no more:
   - the per-language merge steps gate on that language's completeness rather
     than on the whole set;
   - **"Fail incomplete mutation run"** fires only when *no* leg completed. With
     at least one complete leg it becomes a warning naming the incomplete legs,
     and the run continues to summarize what it has;
   - the summarize step is handed the complete legs.
7. The `gate` job keeps failing the workflow when any leg is incomplete — an
   incomplete night is still a red night, and this change does not soften that.
   What changes is that the complete legs' scores are now published and their
   regressions checked before it goes red.
8. Extend `scripts/tests/mutation-workflow.test.sh` with the three summarizer
   cases from Phase 1, step 7.

Nothing else in the workflow moves: no `needs:`, no matrix, no services, no
`shard-budget`, no filename.

### Docs and state

9. [`docs/infrastructure/Testing.md`](../../docs/infrastructure/Testing.md) —
   the mutation section, describing the narrowed survey and per-leg publishing
   as live state. No narration of what it used to do.
10. An ADR in [`docs/adr/`](../../docs/adr/) recording both decisions and the
    0.59% measurement, plus an index row in
    [`.claude/decisions.md`](../decisions.md).
11. Restate the techdebt entry's numbers against the new meaning (D2) — the
    88.2 target is stated against the old survey and does not survive unedited.
12. A `phases.md` Completed row linking `plans/archive/`.
13. **Archive this plan in this same commit** — `git mv` to `plans/archive/`,
    bump every internal relative link one `../` deeper, and re-stage the new
    path (`git mv` stages the pre-edit content, so naming the old path stages
    nothing). Validate with `GO111MODULE=off go run ./scripts/check-doc-links`.

## Phase 3 — Watch (no commit unless reverting)

`workflow_dispatch` a run immediately after the push rather than waiting for the
cron.

Confirm, against Phase 1's local numbers:

- three independent per-language rows in VictoriaMetrics;
- the Go score moved as Phase 1 step 6 predicted;
- the timed-out count did not rise;
- no shard's wall clock moved toward the 90-minute cap.

Then force the property the split exists for: dispatch a run with one leg made
to fail, and confirm the other two still publish.

**Rollback trigger:** any of the four confirmations failing. Revert the commit
whole and re-open this plan with what the run showed. Do not patch forward — a
fix-up commit is the two-commit sequence this plan was written to avoid, arrived
at by accident.

## What this does not fix, stated plainly

- **The Go leg will still be the one that reds.** Narrowing the survey removes
  the harness packages from it, which is where all three of the recently-fixed
  flakes lived — but `internal/api`, `internal/agentapi` and `internal/alerts`
  still run twenty-seven times a night and can still flake. This lowers the
  exposure; it does not remove it.
- **The artifact-loss failure is untouched.** One night was lost to
  `upload-artifact` reporting success with `if-no-files-found: error` set while
  the artifact never appeared in the run (52 shards, 51 shard artifacts). That
  is a platform behaviour and needs its own read-back, in the shape
  [`ci-cd-determinism.md`](../rules/ci-cd-determinism.md) already prescribes for
  caches. **Worth its own entry; not in this plan.**
- **Local pre-push stress testing does not catch this class.** Measured against
  the pre-fix commit: the three flaky tests, twenty attempts each, under four
  conditions (full CPU, `GOMAXPROCS=2`, 24 competing busy loops, and both) —
  the failure did not reproduce. A local rule is not a substitute for either
  change in this plan.

## Reviewer checklist

**Minimality**

- [ ] The job graph is unchanged: no `needs:` edited, no matrix split, no
      service moved, no workflow renamed.
- [ ] Every file in the diff is required by the goal. Anything that is merely an
      improvement was dropped and named in "Minimality — what this change is
      *not*".
- [ ] `complete` still means what it meant; `complete_by_language` is additive.

**Evidence**

- [ ] `gremlins unleash` package-pattern behaviour verified empirically before
      any code was written (Phase 1, step 1).
- [ ] Every number in the diff traces to a Phase 1 command written into this
      plan. Nothing was carried over from a nightly, and nothing waits on a run
      that has not happened.
- [ ] Per-mutant costs measured over the mutants that **finish**, not
      `elapsed / mutants_total` — or recorded as "unchanged" with the
      measurement that showed it.
- [ ] The new leash is comfortably above the slowest finishing mutant on a
      Postgres-backed shard.

**Correctness**

- [ ] "not asked for" and "asked for and broken" stay distinguishable in the
      summarizer (Phase 2, step 5).
- [ ] An incomplete night is still a red night — the gate was not softened.
- [ ] Makefile and workflow gremlins invocations held equal by a test.
- [ ] Go score change after narrowing is explained, not just observed, and the
      techdebt numbers restated against the new meaning.

**Landing**

- [ ] One commit. Docs, ADR, decisions row, phases row and the archive are all
      in it.
- [ ] Phase 3 ran and all four confirmations passed, plus the forced
      one-leg-fails dispatch.
- [ ] If Phase 3 failed, the commit was reverted whole rather than patched
      forward.
