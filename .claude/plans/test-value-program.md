# Test-Value Program — research findings and plan

## Context

The brief: cut unit-test maintenance burden, prefer behaviour-driven journeys,
delete tests that check framework behaviour or functionality that does not
exist, avoid self-verification loops / runtime patching / over-mocking, prove
the suite fails when the code is broken, and write the rule that holds it.

A census was run over all four test trees — **1,526 Go test functions, 862 Rust
tests, 1,554 web tests, 86 shell test files** — and the breakage evidence for
all three compiled languages was taken from the nightly run of
2026-09-01 (commit `86e46bf`), not inferred.

**The census refuted the premise it started from.** The suite carries no
measurable maintenance burden, and the patterns that look wasteful by shape are
mostly not wasteful in fact. What it did find is a small set of provably
worthless tests, **four real defects**, and — once Go and Rust were measured on
the same footing as the web — a **concentration of undetected breakage in the
Rust agent** that the headline scores hide.

---

## 1. Census

| Tree | Tests | Test LOC | Prod LOC | Ratio |
|---|---|---|---|---|
| Go (`server/`) | 1,526 funcs / 402 files | 56,638 | 46,374 | 1.22 : 1 |
| Rust (`agent/`) | 862 (412 inline, 450 in `tests/`) | — | 39,618 | — |
| Web (`web/src`) | 1,554 in 115 files | 18,927 | 17,455 | 1.08 : 1 |
| Shell (`scripts/tests`) | 86 files | 15,830 | — | — |

Go and Rust hold **2,388 of 3,942 unit tests (61%)** and triple the web's test
lines. They are first-class scope here.

## 2. Four measurements that refuted the premise

**2.1 No churn burden.** Across every web test file changed ≥6 times in 12
months: **159 test-file commits against 166 source-file commits — a ratio of
0.96.** Test files change slightly *less* than the code they cover.
`DeviceDetail.test.tsx`: 29 test commits against 35 source commits (0.83).

**2.2 No runtime burden.** The entire web unit suite — 1,554 tests, 115 files —
runs in **15.02 seconds** wall clock (measured, `npx vitest run`).

**2.3 Assertion shape does not predict defect detection.** Grading every web
test by assertion form against the breakage report:

| Test-file weakness by assertion shape | Mean unnoticed-breakage rate |
|---|---|
| ≥30% "weak" assertions (n=5) | **5.8%** |
| <10% "weak" assertions (n=19) | **9.3%** |

The correlation is **inverted**. `use-visible-interval.ts` — 100% "weak" tests
by shape — catches 22 of 24. `format-bytes.ts` — 0% weak, the exemplar —
misses 13.3%. **A rule that deleted tests by assertion shape would delete
working tests and miss the broken ones.**

**2.4 The tiers are not duplicated.** Across 1,554 unit, 32 web integration and
77 end-to-end tests there is **exactly one** duplicated name (`shows active
sessions`) plus ~9 loose pairs. In Go, name overlap between the acceptance tier
and 1,269 unit tests is **zero**.

### What this corrects

An earlier reading claimed 238 worthless presence-only tests, three-fold
duplication of device detail, and a ~29% cut in web test lines. None survive.
Of 358 presence-only blocks, **262 use a query pinning a literal string or
accessible name** — `getByText('Permissions')` throws when absent, so the query
*is* the assertion. Of the remaining 96, most are legitimate negative
assertions. The defensible deletion set is 30 tests, not ~400.

## 3. Does the suite fail when the code is broken? (all three legs)

Nightly run 2026-09-01, commit `86e46bf`:

| Leg | Caught | Unnoticed | Not reached | Rate |
|---|---|---|---|---|
| **Rust** | 2,155 | 299 | — | **88.0%** |
| **Go** | 2,522 | 98 | 314 | **85.9%** |
| **Web** | 3,631 | 521 | 106 | **85.2%** |

**The headline order is misleading.** Rust scores best and is the worst risk,
because its misses are concentrated rather than spread. Go's are spread thin —
no Go file has more than 6 unnoticed breakages, and its caught-rate on covered
code is **96.3%**.

**Rust — concentration, and a fifth of it is not product code.** The raw
ranking puts four bake-off reference modules at the top. They sit behind
`#[cfg(feature = "bakeoff")]`, which the shipped agent disables — traced and
verified in §4.2(f). Separating the two changes the priorities completely:

| File | Missed | Caught | Ships? | What it is |
|---|---|---|---|---|
| `edge-tsdb/src/append_only.rs` | 15 | 28 | **no** | bake-off substrate A |
| `edge-tsdb/src/fault.rs` | 14 | 5 | **no** | fault-injection harness for its own tests |
| `edge-tsdb/src/redb_compact.rs` | 14 | 10 | **no** | bake-off substrate B+ |
| `edge-tsdb/src/redb_store.rs` | 13 | 11 | **no** | bake-off substrate B |
| `mesh-agent-core/src/alerts/evidence.rs` | 16 | 11 | yes | the evidence a technician reads on an incident |
| `alerts/retro/scan.rs` | 23 | 43 | yes | retrospective alert scanning |
| `alerts/retro/mod.rs` | 22 | 49 | yes | — |
| `edge-tsdb/src/store/mod.rs` | 13 | 46 | yes | the shipping on-device store |
| `edge-tsdb/src/store/blocks.rs` | 13 | 79 | yes | — |
| `mesh-agent/src/host_logs.rs` | 12 | 45 | yes | log collection |
| `mesh-agent/src/event_watch.rs` | 11 | 20 | yes | file-change watching |
| `mesh-agent/src/retro_job.rs` | 10 | 13 | yes | retrospective scan job |

**61 of the 299 missed (20%) are in code no customer runs.** On shipping code
the Rust leg is 1,901 caught / 238 missed — **88.9%**.

In RMM terms, the top *real* target is `evidence.rs`: it is what a technician
sees when they open an incident, and more than half the ways to break it go
unnoticed.

**Go — where nothing reaches the code (314 unreached across 79 files):**
`organization/postgres.go` 19 (the customer repository),
`rules/postgres_tags.go` 18, `protocol/control_encode.go` 14,
`rules/catalogue_load.go` 11, `amt/transport/mps.go` 11, `app/app.go` 10,
plus **65 in `tests/loadtest/*`** (the load harness — test tooling, lowest
priority). Go's 98 *unnoticed* are spread thin: worst are
`rules/catalogue_load.go` 6, `inventory/postgres.go` 5,
`lifecycle/orchestrator.go` 5, `rules/catalogue.go` 5.

`internal/rules/stage.go` shows 3 unnoticed — **independent confirmation of
finding (b) below**, arrived at from the source before the data was fetched.

### Two caveats that constrain how this data may be used

**An unnoticed breakage is not automatically a test gap.** All four in
`safe-url.ts`, the URL safety guard, are code that cannot behave differently:
`.trim()` and `trimmed === ''` are unreachable as distinct behaviour because
`new URL()` already rejects both. The finding there is redundant *production*
code, not a missing test.

**Per-test credit cannot be read as a verdict.** The tool records only the first
test to catch each breakage (`killedBy` length is 1 for all 3,631) and runs
files in path order. `safe-url.test.ts` is a model security test — 20 cases
covering `javascript:`, `data:`, `vbscript:`, `file:`, `blob:`, scheme-relative
and whitespace-smuggled schemes — yet is credited with **zero**, because
`features/agent-setup/InstallInstructions.test.tsx` sorts earlier. Only the **30
web tests touching no breakable production code** are safe to read directly.

## 4. Findings

### 4.1 Provably worthless — delete (30 tests)

| What | Where | Why |
|---|---|---|
| 9 `type construction` tests | `web/src/lib/protocol/types.test.ts` | `const msg: ControlMessage = { type: 'RelayReady' }; expect(msg.type).toBe('RelayReady')` — builds a literal, asserts the field it just set. TypeScript types erase at compile time, so no production code runs. A self-verification loop, a framework test and a zero-detection test at once. (The 2 `frame type constants` tests pin the wire bytes against Rust/Go — **keep**.) |
| 5 "initial state" tests | `admin-store`, `file-store`, `chat-store`, `push-store`, `session-store` | Assert a literal equals itself. |
| 1 xterm stylesheet test | `TerminalView.test.tsx` | Asserts a third-party library's stylesheet import. Testing the library. |
| 7 `formatBytes` tests | `DeviceDetail.test.tsx:558-664` | Re-tests six branches through seven full component renders behind three mocks, while `web/src/lib/format-bytes.test.ts` covers every branch directly in 25 lines. |
| **6 trait-impl checks** | `session/handlers/{file,keyboard,mouse,switch,webrtc,terminal_control}.rs` | `fn assert_impl<T: ControlMessageHandler>(); assert_impl::<FileHandler>();` — a compile-time assertion. Non-compliance would not compile, so the body can never fail at runtime. |
| **2 upload tests** | `mesh-agent-core/tests/file_handler_test.rs:32`, `session/handler.rs:702` | `FileHandler::handle_upload` is a documented no-op ("upload not yet implemented", `handlers/file.rs:78`). Testing behaviour the product does not have. |

**Total: 30.**

**Keep, explicitly.** These touch no breakable code only because module-level
constants are skipped, but each pins a client constant against an external
contract — the same seam-test class as Go's `metrics/vocabulary_test.go`:
`incident-lifecycle.test.ts` (3), `rule-coverage.test.ts`, `queue-store`
statuses, `health.test.ts` `HEALTH_META`, `maintenance.test.ts` `MAINTENANCE_META`.

**Also keep the Rust Null-implementation tests** — an earlier draft listed
`null_injector_accepts_mouse_calls` and `test_null_service_lifecycle_does_not_panic`
for deletion. They are **not** the same class as the upload no-op:
`platform-linux/src/lib.rs` selects `NullServiceLifecycle` at runtime on a
machine without systemd, and `NullInput` returns `Err(InputError::NotAvailable)`.
Both are shipped behaviour for container and minimal installs. In RMM terms: a
technician clicks remote desktop on a container-hosted machine and the agent
must decline gracefully rather than crash. Same reasoning keeps the
`*_does_not_panic` tests on real paths (`send_key_on_closed_channel`,
`resize_with_no_terminal`, `handle_candidate_with_no_peer`).

### 4.2 Real defects the census exposed — fix (the valuable half)

**(a) A self-verification loop in the authentication path — most serious.**
`web/src/lib/api.test.ts` copies the production `authMiddleware` **verbatim into
the test file, twice**, and tests the copy. It imports `QUERY_SERIALIZER` but
never imports `api`. Remove `api.use(authMiddleware)`, change the header name,
or drop the `Bearer` prefix — **both tests still pass.** The report confirms it:
`api.ts` carries 10 breakable points; `api.test.ts` reaches none. In RMM terms:
every technician's browser could stop sending its credential, silently.

*How to fix it — verified, and without reshaping production.* The original
author's comment explains why they copied the code: the shared client is built
against a relative base and `new Request('/api/v1/health')` throws. **Confirmed
empirically** — `Failed to parse URL from /api/v1/health`, because Node's
`Request` does not resolve relative URLs even under the jsdom environment. So
"just import the real client" does not work.

An earlier draft proposed exporting a `createApiClient(baseUrl)` factory from
`api.ts` so a test could build the same client against an absolute base. That is
rejected: **a test must not reshape production code to make itself possible.**

The approach that works against the code as it stands: mock `openapi-fetch` —
a genuine third-party boundary — capture what `api.ts` hands to `createClient`
and `.use()` at module load, then invoke the **real** captured middleware with a
real absolute-URL `Request`. That proves the middleware's logic *and* that it is
registered, changing nothing in production. `createClient` also accepts a custom
`fetch` (confirmed in `openapi-fetch@0.17` type definitions) if a fuller
exercise is wanted later.

**(b) A self-verification loop in rule staging.**
`server/internal/agentapi/alert_rules_rollout_test.go:80` computes the expected
canary size by calling the function under test (`rules.StagePopulation(1, estate)`)
and asserts within `want/2 + 3` — a ±50% band around production's own answer.
Independently corroborated: `internal/rules/stage.go` shows 3 unnoticed
breakages. In RMM terms: a curated rule meant to reach 5 machines first could
reach 200 and the gate would pass.

**(c) The session-rehydration path is untested.** `AuthGuard.tsx` leaves four
breakages unnoticed, all in the `useEffect` calling `fetchMe()` when a token
exists but no user is loaded. `AuthGuard.test.tsx` covers the three render
states but never asserts the fetch. In RMM terms: a technician who reloads the
browser mid-incident sits on "Loading…" forever. (The gate itself — redirect to
`/login` with no token — **is** tested; only rehydration is not.)

**(d) The repository layer has no completeness gate for tenant isolation.** The
database layer has an excellent bidirectional one:
`TestTenantIsolationCoversEveryTenantTable` probes every tenant-scoped table and
`TestEveryTenantTableIsProbed` reads the live schema and fails when a table
appears without a probe. **The repository layer has no equivalent.** 30 files
take a tenant-scoped context; 19 test files carry a deny test; nothing fails
when a new tenant-scoped repository ships without one. `organization/postgres.go`
— the customer repository — has **19 unreached breakages**, the most in Go.

**(e) Redundant production code in the URL safety guard.** The four unreachable
breakages in `safe-url.ts` show `.trim()` and `trimmed === ''` cannot behave
differently. Simplify the source; `safe-url.test.ts` stays unchanged and green.

**(f) The Rust agent's blind spots — and a large false alarm inside them.**

**The false alarm, traced to the end.** An earlier draft made
`edge-tsdb/src/fault.rs` the top Rust priority (14 of 19 missed) and then, on
seeing `pub mod fault`, raised it as test-injection code shipping to customers'
machines. **Both were wrong, and the trace is worth recording:**

1. `fault` is declared `#[cfg(feature = "bakeoff")]` — the attribute sits on the
   line above, which a first grep missed.
2. `bakeoff` *is* a default feature of `edge-tsdb`, so that looked unresolved.
3. But `mesh-agent` and `mesh-agent-core` both depend on it as
   `default-features = false, features = ["cold-deflate"]`, and the workspace
   uses `resolver = "2"`, so dev-dependency features do not unify into the
   normal build.
4. Verified rather than reasoned: `cargo tree -e features -p mesh-agent`
   resolves edge-tsdb with **`cold-deflate` only**. `bakeoff` is absent.

**The fault-injection code is not in the shipped agent.** The design is
deliberate and correct; no production change is warranted.

**What the trace did find — the Rust figure is measuring code that never
ships.** Nine edge-tsdb modules are bake-off reference implementations behind
that feature, and `cargo mutants` runs with **default features**, so it mutates
all of them:

| Bake-off module (does not ship) | Missed | Caught |
|---|---|---|
| `append_only.rs` | 15 | 28 |
| `redb_compact.rs` | 14 | 10 |
| `fault.rs` | 14 | 5 |
| `redb_store.rs` | 13 | 11 |
| `baseline.rs` 3, `corpus.rs` 2, `crc.rs` 0, `frame.rs` 0 | 5 | 189 |
| **Total** | **61** | **243** |

So **61 of Rust's 299 missed breakages (20%) are in code no customer runs** —
and every one of the four top targets in the earlier draft was on this list.
Writing tests for them would be testing a bake-off reference, which is exactly
what the brief forbids. `rust-tsdb-substrates` is the shard that sweeps them up
(`"rest"`), and `rust-tsdb-encoding` explicitly names `crc.rs` and `frame.rs`.

**The one real gap the trace leaves:** nothing guards the exclusion. It rests on
every consumer remembering `default-features = false`. The server has exactly
this guard — `faulttest/noship_test.go` inspects the real build graph via
`go list -deps` — and the agent has no equivalent. That is a cheap test, not a
production change.

**Shipping targets, in priority order:**

- **`alerts/evidence.rs::shrink` (14 of its 16 misses) is the priority.** It is
  the ladder that sacrifices evidence to fit a size budget: log samples first,
  then processes, then readings inside each series, then whole series, and the
  ranking last "and never entirely, because it is the line a technician reads
  first." The existing tests
  (`oversized_evidence_is_truncated_and_still_travels`,
  `evidence_that_fits_is_left_alone`) check only the endpoints, never the
  **order of sacrifice**. In RMM terms: on a struggling machine, a technician
  should lose log samples, not the ranked explanation of what went wrong.
- **`edge-tsdb/src/store/` and `compact.rs`** — the shipping store:
  `store/mod.rs` (13 missed / 46 caught), `store/blocks.rs` (13/79),
  `compact.rs` (9/159). These are the real on-device telemetry path, unlike the
  bake-off substrates above.
- **`mesh-agent/src/host_logs.rs` (12/45), `event_watch.rs` (11/20),
  `discovery/ports.rs` (9/18), `connection.rs` (9/14)** — log collection,
  file-change watching, port discovery and the link back to the server.
- **`retro/scan.rs` and `retro/mod.rs`** — `RetroScan::summary` replaced with
  `String::new()` survives, meaning a retrospectively-found incident can reach a
  technician with a **blank explanation** and nothing notices. Same for
  `evidence` and `scope`, plus boundary arithmetic in `RetroPlan::for_rule`.

**All of these are pure logic — no timing, no disk races.** `Instant::now()`
appears once in `scan.rs` (elapsed measurement for pacing) and none of the
missed mutants sit in that path, so tests for them are deterministic.

### 4.3 The "cost-only assertion" cleanup — MOSTLY WITHDRAWN after inspection

An earlier draft proposed rewriting 23 styling assertions and 89 page-structure
walks. Reading all 112 individually shows **the great majority encode real
product behaviour, and rewriting them would lose detection and add flakiness.**

**Styling — 28 of 32 are the product signal, not decoration.** Colour is the
only carrier of state in these components:

| Assertion | Why it is the behaviour |
|---|---|
| `MaintenanceBadge` sky→amber→red (4) | The badge text is the constant "Maintenance"; the colour is the **only** signal of how long a machine has been left in a window. The component's own doc calls it "the visible stand-in for the deliberate absence of auto-expiry." |
| `RuleList` tone (4) | "gives a stopped rule the loudest tone", "does not dress a switched-off rule as one still rolling out" — discriminating between rollout states. |
| `RuleCoveragePanel` red (4) + `rotate-90` (2) | "colours a standing blind spot and nothing else" / "leaves a rule with no blind spot uncoloured"; disclosure open/closed. |
| `SiteSidebar` `ring-2` (6) | Which drop zone is highlighted during a device drag — the only visible feedback. |
| `DeviceLogs` / `DeviceMetrics` active-vs-inactive (5) | Which log facet / time window is selected. |
| `DataLifecycle` green-vs-grey (2), `AmtBadge` blue (1) | Store flag on/off; distinguishing three badge kinds. |

**Genuinely decorative — delete these 4 only:** `'Start Session button uses the
online-green palette'`, `'the System Logs card spans the full grid width'`,
`'the view-logs button uses the Restart-Agent yellow palette'`, and the
`DeviceLogs` paginator palette assertion.

**Page-structure walks — keep essentially all 89.** Classified by purpose:

| Count | Kind | Verdict |
|---|---|---|
| 3 | **XSS tests** — `querySelector('script'/'img'/'b')` + `toBeNull` in `IncidentTimeline` and `AlertEvidencePanel` | **Keep.** A technician's comment or a machine's log line containing `<img src=x onerror=…>` must render as characters. There is no role-based way to assert an element is *absent* — an `img` with no alt has no accessible role. Rewriting would remove the test's teeth. |
| 15 | `closest('a')` on dashboard tiles | **Keep.** Asserts the link target and count behind "Anomalous", "In Maintenance" etc. |
| 17 | `closest('li'/'tr')` row scoping | **Keep.** The standard way to scope an assertion to one row. |
| 7 | `nextElementSibling` for `<dt>`→`<dd>` | **Keep.** No query exists for "the `dd` following this `dt`". (3 disappear anyway with the `formatBytes` deletions.) |
| 53 | mostly `container.querySelector('nav')).toBeNull()` | **Keep.** Legitimate negative assertions; swapping to `queryByRole` is churn with no detection gain and a real ambiguity risk. |

**The `getBoundingClientRect` override must stay.**
`DeviceList.test.tsx:553-566` overrides the global stub that `vitest.setup.ts`
itself installs (jsdom has no layout engine), inside `try/finally` with a
restore, and its comment names the two mutants it exists to kill — the
`width > b.minWidth` EqualityOperator and the `(b) => true` ConditionalExpression.
Removing it loses detection. It is correct code, not a leak.

**What survives from 4.3:** delete the 4 decorative styling assertions, and
split `DeviceDetail.test.tsx` (1,373 lines / 98 tests) by concern for
navigability — its churn is healthy (0.83) and it catches 245 breakages, so it
is not waste, just large.

### 4.4 Cleared — leave alone

- **Go is in excellent shape**: 96.3% caught on covered code, no file above 6
  unnoticed, zero cross-tier name overlap, near-mock-free (2 fakes), zero
  runtime patching.
- **Shell tier is clean.** A heuristic suggested 47 shell tests never execute
  their subject; spot-checking disproved it — they invoke it through a variable
  (`SUMMARIZE="$REPO_ROOT/scripts/loadtest-summarize.sh"`). Not a finding.
- **Go's "19 tests with no assertion" is a parser artifact.** Checked:
  `TestValidateBindingRejectsAValueOutsideTheRulesBounds` and
  `TestCeilingIsPerCustomerNotPerTenant` carry their assertions in helpers
  (`refusesBinding`, `e.record(...)`). Not a finding.
- **`dbtx/tx.go` is not untested.** Its 5 unreached breakages are error and
  rollback branches; `dbtx.Scoped` is called 96 times in production and executed
  by many tests. Not the finding it first appeared to be.
- **Seam tests are legitimate, not self-verification**: `metrics/vocabulary_test.go`,
  `loadtest/telemetry_shape_test.go`, `rules/postgres_rollout_test.go`.
- **Security enforcement is server-side and well covered.** Last-admin
  protection is proven at three layers (repository, HTTP 409, acceptance 403).
  The unnoticed breakages on `Permissions.tsx`'s `disabled=` attribute are a UX
  defect, not a security hole — **the browser is not the security boundary, the
  server is.** That is why browser permission-rendering needs no exhaustive tests.
- **The Go acceptance tier is the model**: one outcome per capability in
  customer words, two doors only, bidirectional capability↔outcome binding.

---

## 5. Standards and hard constraints

**Approved:**

1. **Delete only the provable tests** — 30. No shape-driven deletions.
2. **All four security fixes plus the repository-level gate**, no grandfathering.
3. **Prove by hand (ten breakages) and automatically.**

**Added during the deep review:**

4. **A test must not drive production code.** If code cannot be tested as
   written, find a test approach that works against it; do not add an export, a
   factory, or a seam that exists only for the test. This rejected the
   `createApiClient` idea in §4.2(a) and is why §4.2(f) flags `pub mod fault`
   shipping test-injection code inside the agent library. The exception already
   settled in this repo is the fault-injection seam, which is substituted at
   test time rather than compiled in.
5. **Do not trade detection for tidiness.** §4.3 withdrew a 112-assertion
   rewrite because reading each one showed the assertions carry product meaning.
   A cleanup that lowers the caught-rate is not a cleanup.
6. **Do not add flakiness.** Every new or rewritten test must be deterministic:
   no wall-clock dependence, no reliance on file ordering, no un-restored global
   patch, no query that can match ambiguously. The Rust targets in §4.2(f) were
   chosen partly because their misses are pure logic.

**Re-opened by the deep review:** decision 2 of the earlier round said the hook
should ban styling assertions and page-structure walks. §4.3 shows that would
block XSS tests and the only tests of maintenance escalation, rollout tone and
drop-zone highlight. **The hook's scope needs re-deciding (§11).**

---

## 6. Work plan

Five commits on `dev`, each independently green. Every commit follows the repo
flow: touch the test first (TDD gate), `/precommit`, commit, `/refactor`, push,
reclaim caches.

PR 3 carries an internal split worth noting: its scope change to the Rust
mutation run (§(i)) moves the nightly denominator, so it lands as its own commit
with the new baseline recorded before the Rust tests in §(iii) are written —
otherwise the drop-detector in `scripts/mutation-summarize.sh` reads a scope
change as a regression.

### PR 1 — The rule and its enforcement

- **`.claude/rules/test-value.md`** — states the four banned patterns and,
  just as importantly, **what is deliberately not banned and why**, citing the
  inverted correlation in §2.3 so the next reader does not re-introduce a
  shape-based rule.
- **`.claude/hooks/pretooluse-test-value-guard.sh`** — modelled on
  `pretooluse-test-skip-guard.sh` (same `parse_input_fields` / `block` /
  `enable_fail_closed_hook` structure), registered in `.claude/settings.json`.
  **Its scope is the open question in §11** — the earlier answer (ban styling
  assertions and structure walks) is disproved by §4.3. The candidate that the
  evidence supports refuses two things instead, both of which would have caught
  a real defect found here and neither of which touches a working test:
  a test file that **does not import the module it is named for** (the
  `api.test.ts` class), and a **global or prototype reassignment with no
  restore** (the leak that causes order-dependent flakiness — the legitimate
  `DeviceList.test.tsx` override has a `try/finally` and would pass).
- **`scripts/tests/test-value.test.sh`** — mode **100755** (the shell-tests step
  fails on a non-executable file). Behavioural tests for the hook plus a
  repo-wide sweep, seeded with an allowlist that empties in PR 5.
- **`CLAUDE.md`** index row; **`docs/infrastructure/Testing.md`** gains a
  section on what each tier owns and what a test may assert.

### PR 2 — The security fixes and the new gate (Go + web)

Ordered first among code changes because (a) is live.

- **Rewrite `web/src/lib/api.test.ts`** by the verified route in §4.2(a): mock
  `openapi-fetch`, capture what `api.ts` registers at module load, and invoke
  the **real** middleware with an absolute-URL `Request`. Delete both copied
  middleware bodies. No production change.
- **Fix `alert_rules_rollout_test.go:80`**: assert against an expectation
  derived from the documented staging percentages, not from
  `rules.StagePopulation`; tighten the ±50% band.
- **Cover rehydration** in `AuthGuard.test.tsx`.
- **Add the repository-level completeness gate**, mirroring
  `TestEveryTenantTableIsProbed`: enumerate packages whose production code takes
  a tenant-scoped context and fail when one carries no deny test. Write the
  deny tests it names — `organization/postgres.go` first, being the worst
  Go gap at 19 unreached — following `notifications/webpush_test.go` as the
  reference shape.
- **Simplify `safe-url.ts`**, removing `.trim()` and the `=== ''` branch.

### PR 3 — Rust: close the concentrated blind spots

The largest genuine risk found, and the answer to why Rust belongs in scope.
Two halves: stop measuring code that does not ship, then close the gaps in code
that does.

**(i) Take the bake-off modules out of the mutation scope.** Nine edge-tsdb
modules sit behind `#[cfg(feature = "bakeoff")]`, which the shipped agent
disables (§4.2(f), verified via `cargo tree -e features -p mesh-agent`). Add
`--no-default-features --features cold-deflate` to the edge-tsdb shards in
`scripts/lib/mutation-shards.sh`, or exclude the nine paths in
`agent/.cargo/mutants.toml` with the per-entry justification the file already
uses. This removes **61 missed and 243 caught** phantom results and stops the
Rust figure describing a bake-off reference.

**(ii) Guard the exclusion.** Add the agent's missing equivalent of
`server/internal/faulttest/noship_test.go`: assert from the real build graph
that `bakeoff` is off for `mesh-agent`, so a future crate depending on edge-tsdb
with default features cannot silently pull the fault harness into what ships.
A test, not a production change.

**(iii) Close the shipping gaps**, priority order per §4.2(f), all deterministic:

1. `alerts/evidence.rs::shrink` — assert the **order of sacrifice**, not just
   the endpoints: an oversized payload loses log samples before processes,
   processes before readings, readings before whole series, and the ranking last
   and never entirely.
2. `retro` — `RetroScan::summary` / `evidence` / `scope` return real content,
   and `RetroPlan::for_rule` boundary arithmetic.
3. `edge-tsdb/src/store/mod.rs`, `store/blocks.rs`, `compact.rs` — the shipping
   store path.
4. `mesh-agent/src/host_logs.rs`, `event_watch.rs`, `discovery/ports.rs`,
   `connection.rs`.

For each: read `mutants.out/missed.txt` from the nightly artifact, triage every
entry into *genuinely unreachable* or *real gap*, test the gaps, and record the
unreachable ones per-entry in `agent/.cargo/mutants.toml`. Re-run
`cargo mutants --package <crate>` and require the missed count to fall.

**Expected side effect to state up front:** (i) changes the Rust denominator, so
the nightly figure will move for a reason unrelated to test quality. Land it in
its own commit with the new baseline recorded, so the drop-detector in
`scripts/mutation-summarize.sh` is not tripped by a scope change.

### PR 4 — Delete the 30

Exactly the table in §4.1, nothing else; each deletion names its reason in the
commit body. Update the equivalent-mutant note in `agent/.cargo/mutants.toml`,
which already documents the upload arm as uncatchable.

### PR 5 — The assertion residue, and closing the loop

PR 5 and the former PR 6 are one commit. The archive rule wants a plan retired
in the same commit that lands its final implementation
(`plans-archive-consistency.test.sh` refuses a `phases.md` **Completed** row
pointing at a non-archived plan), so the last code change and the paperwork
belong together rather than in two commits.

**The code residue** — scope cut hard by §4.3:

- Delete the **4 genuinely decorative** styling assertions.
- Split `DeviceDetail.test.tsx` (1,373 lines / 98 tests) by concern —
  hardware, power/AMT, sites/customers, sessions — moving tests verbatim and
  sharing one setup helper so the split cannot silently change behaviour. Run
  the file's mutation check before and after; the caught count must be identical.
- Remove the PR-1 allowlist so the sweep asserts zero.

**Explicitly not done:** the 89 page-structure walks and the other 28 styling
assertions stay. They carry product meaning, three of them are XSS tests, and
rewriting them to role queries would lose detection and risk ambiguous matches.

**Closing the loop, same commit:**

- `.claude/phases.md` Completed row.
- `git mv` this plan to `.claude/plans/archive/test-value-program.md`, links
  bumped one `../` deeper, and **re-stage the new path** — `git mv` stages the
  pre-edit content, so naming the old path stages nothing. Validate with
  `GO111MODULE=off go run ./scripts/check-doc-links`.
- ADR in `docs/adr/` — "a test asserts the result the user gets, and assertion
  shape is not evidence of value" — plus its row in `.claude/decisions.md`.

Ordering inside the commit matters: land the code edits first, run `/precommit`,
then do the archive move and index updates, because the doc-links gate and the
archive-consistency gate both read the final tree.

---

## 7. Verification

### 7.1 Ten hand-breakages

Break the real code, run the tests, confirm red, revert. Four are marked
**green today** — they are the acceptance criteria for the PR-2 and PR-3 fixes,
and their going red is the proof each fix landed.

| # | Break this | What a technician would see | Must fail |
|---|---|---|---|
| 1 | `format-bytes.ts` — stop the unit ladder at GB | A 3 TB fileserver reads "1024 GB" | `format-bytes.test.ts` |
| 2 | `DeviceDetail.tsx` — bind disk *free* where disk *total* belongs | A full disk reads as empty | device-detail tests |
| 3 | `api.ts` — remove `api.use(authMiddleware)` | Every browser request goes out with no credential | **green today** → api client test |
| 4 | A repository query — drop its tenant clause | One customer's machines appear under another | tenant isolation + repo deny test |
| 5 | Log-pull authorisation — return 200 with an empty body instead of 403 | A member reads logs they may not see | `TestATechnicianWithoutElevatedPermissionCannotPullALog` |
| 6 | Enrolment — stop incrementing the token use count | An exhausted token keeps enrolling machines | `TestAnExhaustedEnrolmentTokenIsRefused` |
| 7 | Maintenance window — invert the check | Alerts fire through a customer's approved patch window | maintenance / alerts tests |
| 8 | `rules.StagePopulation` — ignore the stage percentage | A canary meant for 5 machines reaches all 200 | **green today** → rollout test |
| 9 | `redact_log_line` — disable one secret shape | A password in a customer's log reaches the technician's screen | Rust redaction tests + `TestATechnicianPullsALogAndTheSecretInItNeverReachesThem` |
| 10 | `alerts/evidence.rs` — drop one evidence item from an incident | A technician opens an incident and the reading that caused it is missing | **green today** → PR-3 tests |

Rehydration (fix c) carries its own: delete the `useEffect` in `AuthGuard.tsx`
and the suite must go red — **green today**.

Record each outcome in the PR body. A breakage that stays green is a real gap:
close it in that PR before deleting anything nearby.

### 7.2 Automatic

Re-run the tool on the files each batch touches — `npx stryker run --mutate
<file>` (web), `make mutate-go`, `cargo mutants --package <crate>` (Rust,
`OPENGATE_GOLDEN_DIR` set by `make mutate-rust`). The caught-rate must not fall.
Treat each reported unnoticed breakage as a candidate needing judgement —
genuinely unreachable code, or a real gap — never as an automatic gap (§3).

**Headroom warning:** `scripts/mutation-summarize.sh` alerts below **85.0%** or
on a drop over 2 points. Web sits at **85.2%** and Go at **85.9%** — 0.2 and 0.9
points of room. PR 4's deletions must be verified file-by-file, not in one sweep.

### 7.3 Whole suite

`/precommit` on every commit (hook-enforced, no marker bypass), then `/refactor`
before push. The ≥80% line coverage floor on all three legs must hold; the 30
deletions are chosen to touch no uniquely-covered line, but if one dips, write
**one** test covering those lines *and* catching a breakage — never restore the
deleted test.

---

## 8. Security and long-term maintainability

**Security.** This program improves posture rather than trading it away. It
closes a live hole where the credential-attaching code is not exercised by its
own test, adds the missing completeness gate so a new tenant-scoped repository
cannot ship unproven, and closes the Rust agent's concentrated blind spots. The
principle it writes down — the browser is not the security boundary, the server
is — is what lets browser permission-rendering stay lightly tested without risk.

**Maintainability as the fleet grows.** Upkeep is already low (churn 0.96,
runtime 15s), so the durable win is not a smaller suite but a **rule that stops
the next reader "cleaning up" by shape** — which the measurements show would
remove working tests. The completeness gates (capability↔outcome,
tenant-table↔probe, now repository↔deny test) keep cost proportional to
capability as the estate grows.

**RMM edge cases the journey layer must keep covering** — verify present before
touching a neighbouring test: a machine offline mid-session; a machine with a
wrong clock; a rebuilt machine with a new certificate; a customer with exactly
one site; an agent behind the manifest; a machine that never reported inventory;
an approved maintenance window; a token exhausted or unknown.

---

## 9. Files

**New:** `.claude/rules/test-value.md`,
`.claude/hooks/pretooluse-test-value-guard.sh`,
`scripts/tests/test-value.test.sh` (mode 100755), one ADR in `docs/adr/`, a
repository-level tenant-deny completeness test under `server/internal/dbtx/`
(beside `scoped_sql_test.go`), and new Rust tests under
`agent/crates/mesh-agent-core/tests/` and `agent/crates/edge-tsdb/tests/`.

**Edited:** `.claude/settings.json`, `CLAUDE.md`, `.claude/phases.md`,
`.claude/decisions.md`, `docs/infrastructure/Testing.md`,
`agent/.cargo/mutants.toml`, `web/src/lib/safe-url.ts`.

**Test files changed:** `web/src/lib/api.test.ts` (rewrite),
`web/src/features/auth/AuthGuard.test.tsx`,
`server/internal/agentapi/alert_rules_rollout_test.go`,
`web/src/lib/protocol/types.test.ts`, the five store `initial state` tests,
`web/src/features/terminal/TerminalView.test.tsx`,
`agent/crates/mesh-agent-core/tests/file_handler_test.rs`,
`agent/crates/mesh-agent-core/src/session/handler.rs`, the six
`session/handlers/*.rs` trait checks plus `handlers/mouse.rs` and
`platform.rs` Null smoke tests,
`web/src/features/devices/DeviceDetail.test.tsx` (delete 7, split),
plus the §4.3 styling / structure / patching files.

**Reused, not rebuilt:** `scripts/mutation-summarize.sh` (the automatic check),
the nightly artifacts (`mutants.out/missed.txt`) as the Rust work list,
`server/internal/db/tenant_isolation_test.go` (model for the new gate),
`server/internal/dbtx/scoped_sql_test.go` (architecture-check pattern),
`web/src/lib/format-bytes.test.ts` and `safe-url.test.ts` (model unit tests),
`server/tests/acceptance/` (model outcome naming),
`.claude/hooks/pretooluse-test-skip-guard.sh` (hook template).

## 10. Flakiness review

The suite's existing hygiene is strong and must not be degraded: **zero** web
test files use a store without resetting it in `beforeEach`; Go runs
`-race -count=1`; Playwright uses `retries: 0` on the gate path; CI deliberately
does not auto-close failure issues so flakes cannot be masked.

Risk assessed per change:

| Change | Flakiness risk | Mitigation |
|---|---|---|
| Delete the 30 (§4.1) | **None** — removal only, and none is the sole cover of a line. | Per-file caught-count check before/after. |
| `api.test.ts` rewrite | **Low.** Mocks a third-party module at load; no timers, no network. | Absolute URL in the `Request`, so no environment dependence. |
| Rollout self-verification fix | **Low.** Replaces a ±50% band with a fixed expectation. | Derive from documented percentages, keep `t.Parallel()`. |
| AuthGuard rehydration test | **Medium** — asserting an async effect. | Use the existing `waitFor` idiom already used across the suite; no fake timers, no fixed sleeps. |
| Repository tenant-deny gate | **Low**, but the enumerator is the risk: a source scan can drift. | Model it on `TestEveryTenantTableIsProbed`, which fails *both* ways — an unprobed package and a stale entry. |
| Rust targets (§4.2(f)) | **Low.** Chosen because the misses are pure logic — boundary comparisons, the shrink ladder, return values. | Explicit byte caps and temp dirs, never elapsed time. `Instant::now()` appears once in `scan.rs` and no target sits in that path. |
| `DeviceDetail.test.tsx` split | **Medium** — a split can silently change shared setup. | Move tests verbatim, share one setup helper, require an identical caught-count after. |
| The 4 decorative deletions | **None.** | — |

**Withdrawn precisely because it would have added flakiness:** converting 89
structure walks to role queries. `getByRole` can match several elements and
throws on ambiguity, so the rewrite would trade deterministic
`closest('li')`/`querySelector` lookups for queries that break as the DOM grows
— for zero detection gain.

## 11. Approved: the hook refuses two evidence-backed patterns

The earlier answer ("ban styling assertions and page-structure walks") was
disproved by §4.3 and is withdrawn. The hook refuses instead:

1. **A test file that does not import the module it is named for** — exactly the
   `api.test.ts` defect, where production code was copied into the test and the
   copy was tested. Three other files trip a softer version of this
   (`device-drag`, `queue-store`, `organization-store` each leave one export
   unimported); the guard targets the primary export.
2. **A global or prototype reassignment left un-restored** — the leak that makes
   a test pass or fail depending on what ran before it. The legitimate
   `DeviceList.test.tsx` layout override has a `try/finally` restore and passes.

Everything else in §4.3 is advisory in the gauntlet sweep, never a block.

## 12. What this program is, in one paragraph

Not a cull. The census found the suite healthy — upkeep below its own source
churn, a 15-second web run, and no meaningful cross-tier duplication — so the
value here is: **delete 30 tests that provably cannot fail; fix four real
defects the measurement exposed, one of them in the credential path; stop the
Rust figure measuring 61 breakages in a bake-off reference no customer runs;
close the shipping gaps that matter to a technician reading an incident; and
write the rule that stops the next reader "tidying up" by assertion shape,
which the evidence shows would delete working tests.** Four separate
would-be-changes were withdrawn during the review precisely because they would
have lost detection or added flakiness — that record is kept in §4.3 and
§4.2(f) so the same ideas are not re-proposed.
