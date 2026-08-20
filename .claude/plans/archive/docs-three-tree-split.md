# Master plan — one fact, one home

Split `docs/` into product / architecture / infrastructure, then bring the
state files under the same rule.

Status: **finalized, no open questions.** Awaiting approval to implement.
**One commit, one push — a hard requirement (D18).**

Revised 2026-08-19 against the tree as it stands after the edge-first telemetry
and investigations programme closed out. Every figure below was re-measured, not
carried forward.

One rule drives all three workstreams: **a fact has one home, and everything
else links to it.** Today the same fact is written out in a build-or-run chapter,
again in a state file, and again on the front page.

| Workstream | Scope | Steps |
|---|---|---|
| **A** | The three-tree split of `docs/` | S1 … S7 |
| **B** | Bringing [`decisions.md`](../../decisions.md), [`phases.md`](../../phases.md) and [`techdebt.md`](../../techdebt.md) under the same rule as the ADRs | B1 … B5 |
| **C** | The root [`README.md`](../../../README.md) — a page that sells the product, with the mechanism it currently recites moved to the tree that owns it | C1 … C3 |

They are ordered, not separable — see *Execution* for why A must precede B and C.

## Problem

`docs/` is organized by engineering concern, so every product capability is
described inside a build-or-run chapter. There is no product documentation set.
A reader asking what OpenGate tells a technician when a customer's file server
starts thrashing has to read a chapter titled *Monitoring & Observability* that
opens with Grafana port-forwards.

Three defects compound:

1. **Mixing.** [`Monitoring.md`](../../../docs/infrastructure/Monitoring.md) is 1097 lines, of
   which roughly 660 are product surface (vitals, alerts, rules, correlation,
   incidents, maintenance mode, the web telemetry pane) and roughly 440 are the
   observability stack. [`Architecture.md`](../../../docs/architecture/Overview.md),
   [`Wire-Protocol.md`](../../../docs/architecture/Wire-Protocol.md) and
   [`Database.md`](../../../docs/architecture/Database.md) carry the same hybrid.
2. **Duplication.** The same fact has two homes and drifts in one — see the
   duplication ledger below.
3. **History and forward-looking prose.** Enumerated below with file:line.
   [`.claude/rules/docs-live-state.md`](../../rules/docs-live-state.md) forbids all
   of it, but nothing enforces the rule, and it has been re-violated repeatedly.

And the same defect is larger outside `docs/` than inside it:

4. **The state files re-tell the ADRs.** [`decisions.md`](../../decisions.md) calls
   itself a compact index in its own header and is 87 KB across 74 rows — a
   median row of 783 characters, and **85 % of a row's distinctive terms also
   appear in the ADR it points at** (60 of 63 rows above 70 %). Verbatim overlap
   is only 5 %, which is the worse case, not the better one: the rows are
   *paraphrases*, so the two copies drift independently and no diff ever shows
   it. [`phases.md`](../../phases.md) is 235 KB across 159 rows — median 1 550
   characters, longest 4 569 — sharing 59 % of its terms with the archived plan
   each row links and 54 % with the ADRs each row cites. Measurements and method
   in *Workstream B* below.

## What the revisit changed

The programme that closed on 2026-08-19 added a product surface the first draft
of this plan could not have accounted for. Seven corrections follow from it.

| # | Finding | Effect on the plan |
|---|---|---|
| R1 | `Monitoring.md` grew 881 → 1097 lines. Two whole sections are new: **Incidents: the room an alert lands in** (656–743) and **Watching the rule pack itself** (744–818) | Every §line range in the old product table was stale. All re-derived below |
| R2 | [`Rule-Administration.md`](../../../docs/product/Rule-Administration.md) landed 2026-08-17 as a chapter — 177 lines, already product-shaped | New chapter in the inventory; D7 settles its seam against `Alerts-and-Rules.md` |
| R3 | Investigations became a full capability: the queue, the room, the lifecycle, the closed cause-code set, the device incident strip. It spans `Monitoring.md`, `Architecture.md` §Web Client Features, `API-Reference.md` §Investigations, `Database.md` §Investigation Tables, and ADR-075/077/078 | D6 gives it its own product chapter |
| R4 | **Web push carries no alert path.** The notifier's event set is `device_online`, `device_offline`, `session_started`, `session_ended` ([`notifier.go:16-22`](../../../server/internal/notifications/notifier.go#L16)) and `server/internal/alerts` references no notifier at all | The old plan's "web push is how an alert reaches a person" was wrong. D9 re-routes it |
| R5 | The plan assigned `Monitoring.md` 174–360 to `Device-Health.md` **and** "what an active series costs" to infrastructure — the second sits at 257–347, inside the first | D12 settles it as infrastructure, on evidence: the section links [`values.yaml`](../../../deploy/helm/monitoring/values.yaml) twice, at 337 and 342 |
| R6 | The old blast-radius figure ("~85 ADR files") was wrong by 4×, and four `.claude/rules/*.md` files carrying chapter links were missed entirely | Re-counted in *Move cost* below: **~147 link occurrences**, exact inventory |
| R7 | The gate's proposed phrase list produces **five false positives** on live prose today, so its stated target — zero hits, no allowlist — was unreachable as written | D11 replaces the list with one measured to zero false positives |

Three ADRs landed in the window and are inbound-link sources like any other:
ADR-077, ADR-078, ADR-079. The corpus is 63 per-file ADRs.

## Locked decisions

| # | Decision | Consequence |
|---|---|---|
| D1 | **Three trees**: `docs/product/`, `docs/architecture/`, `docs/infrastructure/` | A two-way split would force Wire-Protocol and Database in beside Terraform and CI, where they do not belong — they are the product's implementation, not the ground it runs on |
| D2 | **Product tree is developer-facing**, organized by capability | Existing conventions (link-don't-paraphrase, code links, ADR references) keep applying, so text is lifted rather than rewritten |
| D3 | **`Multiscale-Readiness.md` leaves `docs/` whole** | It is planning content and cannot satisfy live-state. Lands in `.claude/plans/archive/multiscale-readiness.md` — see the routing note below. Re-checked 2026-08-19: the programme that closed was edge-first telemetry; this file is the >20k-agent scale-out rebuild spec and is untouched by it, so D3 stands |
| D4 | **Both gates ship** in S7: a seam guard and a live-state guard | "Live state only" becomes a gauntlet failure instead of an aspiration |
| D5 | **One commit, one push, for all three workstreams.** Subsumed into D18 | No intermediate state where `docs/` is half-split, the state files are half-consolidated, or the front page advertises a chapter that has moved |
| D6 | **`product/Investigations.md` is its own chapter** | The response half of the alert story is now larger than the detection half's evidence section: grouping, the queue, the room, four statuses, seven cause codes, auto-resolve, the maintenance shield. Folding it into `Alerts-and-Rules.md` would produce the single largest chapter in the tree and re-create exactly the mixing this split exists to end |
| D7 | **`Rule-Administration.md` stays a separate chapter.** The seam against `Alerts-and-Rules.md` is **model vs. surface** | `Alerts-and-Rules.md` owns what a rule *is* — grammar, catalogue, bindings, coverage states, rollout mechanics, retroactive scan, evidence. `Rule-Administration.md` owns what an administrator *sees and may change* — tuning, label precedence, the noise badge, rollout pace, the stop switch, alert ceilings. The file already opens by drawing that line ("What cannot be changed here"), so the seam is the one it was written to |
| D8 | **No `Incident-Correlation.md` chapter.** "Ranking what broke" (`Monitoring.md` 626–655) folds into `Alerts-and-Rules.md` | The section describes the agent *producing* a ranking that "travels with the alert" — it is how evidence is made, not how a room is read. This resolves the old plan's one explicitly deferred question |
| D9 | **Notifications is not an alert path.** The product half folds into `Fleet-and-Devices.md`; VAPID, the `Notifier` interface, `PushNotifier`/`NoopNotifier` and the service worker stay in `architecture/Overview.md` | Per R4. `Alerts-and-Rules.md` states positively how an alert reaches a person — it opens or joins an incident, and the queue is where somebody finds it |
| D10 | **`Architecture-Decision-Records.md` stays at `docs/` root**, exempt from the seam gate alongside `README.md` and `Home.md` | It is the ADR corpus, not a chapter. Relocating it into `adr/` is a separate decision about ADR numbering and this split will not smuggle it in. Without this the seam gate would fail on the file on its first run |
| D11 | **Live-state gate scans chapters only** — `docs/**` minus `docs/adr/**` and `docs/Architecture-Decision-Records.md` — with the tightened phrase list in *The two gates* | An ADR's Context section is *required* to state the problem the decision solved, so past-state is structural there, and [`docs/README.md`](../../../docs/README.md) §2 forbids deleting substantive ADR rationale. ADR-036 line 118 quotes the banned phrasing as its own worked example of what to fix. This is the same document-class boundary [`check-doc-links`](../../../scripts/check-doc-links) already draws around `.claude/plans/**` — a scope definition, not an allowlist |
| D12 | **"What an active series costs the central store" is infrastructure** | Per R5. The section sizes VictoriaMetrics against its memory limit and its volume, and links the Helm values twice to do it. The product-side fact it establishes — a device reports at most 24 series — is stated in `product/Device-Health.md` and linked from here |
| D14 | **The ADR is the only home of a decision and its why.** `decisions.md` is an index, `phases.md` is a ledger, `techdebt.md` is a register of what is still owed | Each is a pointer with just enough text to choose a link. Rationale lives once, in the ADR, where [`docs/README.md`](../../../docs/README.md) §2 already governs it |
| D15 | **Rescue before cut.** No row is shortened until its distinctive terms are checked against the ADR it points to, and anything the ADR does not carry is **moved into the ADR first** | Consolidation must not be deletion. Probed across all 63 rows: 14 carry nothing the ADR corpus lacks, and the rest average ~8 unmatched terms each, mostly ordinary English plus a few real identifiers (`alerts_suppressed_total`, `incidents_open`, `dyncfg`, `hysteresis`). Small, and cheap to check row by row — but the check is the step that makes B safe |
| D16 | **Caps, not guidance.** A `decisions.md` row is ≤ 200 characters of prose; a `phases.md` row is ≤ 300 | These files grew to 322 KB under a convention nobody could fail. A number a test enforces is the only version of this rule that survives the next busy week |
| D17 | **Archived plans are not consolidated, and not cited from durable docs** | They are the working document an engineer followed, deletion-bound by [`plans-and-adrs.md`](../../rules/plans-and-adrs.md). Nothing durable may depend on one, so there is nothing in them to merge — the fix is that `phases.md` stops re-telling them and just links |
| D18 | **A, B and C land in one commit and one push.** Hard requirement, set by the user on 2026-08-19 after the multi-commit alternative was put and declined | The gauntlet runs as a commit-time guard, so a failure means the commit never existed — fix and re-attempt the **same** commit. Retries are not extra commits and do not weaken the requirement. The costs are real and stated rather than argued: `git diff -M` reports no clean renames for the 18 moved chapters, and the reconciliation checks stop being advisory — they are the only evidence nothing was dropped |
| D19 | **The root `README.md` sells; `docs/` explains.** No protocol name, library name, algorithm name, schema detail or stack table on the front page | A reader arriving at the repo is deciding whether this product is interesting, not how it is built. Every mechanism the page recites today already has a home in `docs/` after A — so removing it from the README is the same one-fact-one-home move as the rest of the plan, not a loss |
| D20 | **Nothing is cut from the README until it is confirmed present in `docs/`** | The same rescue discipline as D15, applied to a different file. The README carries claims the docs may not — anything unmatched moves into the owning chapter before the front page loses it |
| D13 | **The mermaid floor drops 12 → 11** | `Multiscale-Readiness.md` carries one fence and leaves with it. 11 is exactly the pinned set: Overview 5, Wire-Protocol 1, Kubernetes 1, Continuous-Deployment 1, Monitoring 1, ADR-025 1, README 1. The file appears in no row of the diagram coverage standard, so the standard is unchanged |

### D3 routing note

The destination is `.claude/plans/archive/` rather than `.claude/plans/`, because
[`plans-and-adrs.md`](../../rules/plans-and-adrs.md) permits an ADR to link a plan
**only** under `plans/archive/…`, and four ADRs link this file today.

Full inbound inventory — the old plan counted the ADRs and missed the rest:

| Source | Occurrences | Action |
|---|---|---|
| `docs/adr/ADR-023` ×2, `ADR-030` ×2, `ADR-034` ×1, `ADR-040` ×3 | 8 links | Repoint to `../../.claude/plans/archive/multiscale-readiness.md` |
| [`.claude/decisions.md`](../../decisions.md) rows 023, 034 | 2 links | Repoint |
| [`.claude/phases.md`](../../phases.md) rows WS-15b, WS-15, Dormant Teardown | 3 links | Repoint |
| [`docs/Home.md`](../../../docs/Home.md) index row | 1 | Delete the row |
| [`docs/Kubernetes.md:93`](../../../docs/infrastructure/Kubernetes.md#L93) | 1 | Delete the link; fold the requirement sentence inline |
| [`docs-diagrams.test.sh:70`](../../../scripts/tests/docs-diagrams.test.sh#L70) | 1 pin | Remove with the file (D13) |

`docs/Kubernetes.md:93` must drop its link either way — non-ADR docs under
`docs/` may link no plan at all. `.claude/**` files outside `docs/` may link
`plans/archive/…`, so their repoint is a path change, not a deletion. References
from inside `.claude/plans/**` are out of `check-doc-links` scope and are left
alone.

## The seam

One test decides every paragraph:

> Does this describe something a technician or a customer can see or do — or
> does it describe how we build, deploy and run it?

Semantics on one side, mechanism on the other. "A stall vital is the share of the
last minute tasks spent waiting on disk" is product. "It is a gauge scraped into
VictoriaMetrics with stream aggregation producing avg-only rollups" is
infrastructure. Those two sentences are adjacent paragraphs today, which is
precisely why the duplication exists.

The seam cuts cleanly through the two new sections. "Watching the rule pack
itself" is five aggregate series on `/metrics` read by a Grafana dashboard *we*
run — infrastructure. Its customer-facing counterpart, the noise badge on the
rules list, is already in `Rule-Administration.md` — product. Neither restates
the other.

## Target tree

```
docs/
├── README.md                            conventions (unchanged location)
├── Home.md                              three index tables, one per tree
├── Architecture-Decision-Records.md     ADR-001 … ADR-012 log (D10)
├── product/                             what the system does
├── architecture/                        how it is built
├── infrastructure/                      how it runs
├── adr/                                 unchanged
└── api/                                 unchanged
```

### `docs/product/` — 11 chapters

| Chapter | Content | Lifted from |
|---|---|---|
| `Fleet-and-Devices.md` | Dashboard, device list, device detail, customer picker, sites, hardware inventory, discovered footprint, the device incident strip, browser notifications for device online/offline | `Architecture.md` §Web Client Features, §Notifications (product half, D9); `Database.md` §§Device Hardware / Device Inventory / Device Processes (semantics only) |
| `Remote-Sessions.md` | Session view, desktop, terminal, files, chat, capability-based tab visibility, WebRTC upgrade, session-lifecycle notifications | `Architecture.md` §§Web Client Features / Agent Session Handler / WebRTC Upgrade / Session Lifecycle (product half), §Notifications (session events) |
| `Device-Health.md` | The vitals contract, per-mount disk semantics, stall vitals, disk-performance vitals, unsupported-source behavior, the 24-series-per-device ceiling, maintenance mode, the web telemetry surface, reconnect backfill and deep-history pull | `Monitoring.md` 118–125, 174–256, 348–377, 903–966 |
| `Alerts-and-Rules.md` | Threshold alerts, system-event rules, rule grammar, catalogue and bindings, coverage states, staged rollout, retroactive evaluation, alert evidence, ranking what broke (D8), and how an alert reaches a person — it opens or joins an incident (D9) | `Monitoring.md` 378–655; `Wire-Protocol.md` §§Alert rules, breaches and coverage / Alerts and their evidence; `Database.md` §Rule Configuration Tables |
| `Rule-Administration.md` | Whole file moves. Tuning, label precedence, the noise badge, rollout pace, the stop switch, alert ceilings (D7) | `docs/Rule-Administration.md` |
| `Investigations.md` | Incident grouping and its two axes, the triage queue, the room, the four-status lifecycle, the seven cause codes, auto-resolve, the maintenance shield, evidence decode (D6) | `Monitoring.md` 656–743; `Architecture.md` §Web Client Features (Investigations rows + the snapshot paragraph); `API-Reference.md` §Investigations (semantics); `Database.md` §Investigation Tables (semantics) |
| `Endpoint-Logs.md` | System Logs pane, on-demand host log pulls, the transient broker, redaction, audited reads | `Monitoring.md` 136–161; `Database.md` §Device Logs |
| `Intel-AMT.md` | AMT power actions, device tracking, what an admin can do with an AMT device | `Architecture.md` §Intel AMT MPS (product half); `Database.md` §AMT Devices Table (semantics) |
| `Agent-Updates.md` | Whole file moves | `docs/Agent-Updates.md` |
| `Tenancy-and-Access.md` | Four-level tenancy, customers and sites, security groups and RBAC, user management, the admin settings surface, audit log | `Architecture.md` §§Settings / Audit Log; `Database.md` §§Tenancy / Multi-Tenancy / Security Groups (semantics) |
| `Data-Erasure.md` | Whole file moves, renamed | `docs/Data-Lifecycle.md` |

### `docs/architecture/` — 5 chapters

`Overview.md` (`Architecture.md` minus its product sections), `Wire-Protocol.md`
(minus alert semantics), `API-Reference.md`, `Database.md` (driver, pool, schema
types, schema, migrations, RLS mechanism, transport security),
`Platform-Abstraction.md`.

The AMT CIRA/APF handshake diagram and its design decisions stay in
`architecture/Overview.md`; only the operator-facing capability moves to
`product/Intel-AMT.md`. All five of `Architecture.md`'s mermaid fences stay —
they sit under System Context, Container View, Agent → Server, WebSocket Relay
and Session Lifecycle, none of which crosses the seam.

### `docs/infrastructure/` — 10 chapters

`Kubernetes.md`, `OCI-Terraform.md`, `CI-Pipeline.md`, `Continuous-Deployment.md`,
`Container-Images.md`, `Monitoring.md`, `Security-and-Dependencies.md`,
`Testing.md`, `Fault-Injection.md`, `Shell-Quality.md`.

`infrastructure/Monitoring.md` keeps: overview, architecture, sources of truth,
components, storage model, access, the instrumentation transport (110–135,
162–173), what an active series costs (257–347, per D12), watching the rule pack
itself (744–818), telemetry load and observability (819–902), the long-term tier,
dashboards and alerts, deployment and validation, ad-hoc investigation, and the
CI trend metric convention.

### Chapter filenames — three renames

| From | To | Why | Inbound refs |
|---|---|---|---|
| `Architecture.md` | `architecture/Overview.md` | It is the tree's front door and used that way — listed first in `Home.md`. `Overview` names that role; `architecture/Architecture.md` stutters and says nothing extra | 21 files |
| `Infrastructure.md` | `infrastructure/OCI-Terraform.md` | The chapter is the cloud account, Terraform resources, bastion access, secrets and config validation — not the whole subject its old name claimed, and not a name that survives sitting inside a tree called `infrastructure/` | 16 files |
| `Data-Lifecycle.md` | `product/Data-Erasure.md` | The chapter is right-to-be-forgotten erasure specifically — tombstone deny-list, purge state machine, reconciliation sweep. "Data lifecycle" reads as retention policy, which lives elsewhere | 7 occurrences |

Every other chapter keeps its filename. `git mv` keeps `git log --follow` intact
across both the moves and the renames.

`Architecture.md` costs more than the other two on top of the rename:

- [`docs-diagrams.test.sh:68`](../../../scripts/tests/docs-diagrams.test.sh#L68)
  pins five mermaid fences to it by path.
- The diagram coverage standard in [`docs/README.md`](../../../docs/README.md) names
  it in **four of its six rows** (system context, container topology, protocol
  flows, session lifecycle). That prose gets repointed to
  `architecture/Overview.md` in S1, not left to S7.

`OCI-Terraform.md` and `Data-Erasure.md` have no diagram pin and no
coverage-standard row.

## Duplication ledger — one fact, one home

| Fact | Homes today | Single home after |
|---|---|---|
| `mem.used` / `disk.used` resolve to the percent dims | [`Monitoring.md:406`](../../../docs/infrastructure/Monitoring.md#L406), [`Wire-Protocol.md:224`](../../../docs/architecture/Wire-Protocol.md#L224) | `product/Device-Health.md`; Wire-Protocol lists wire names only and links |
| The host-resource dimension vocabulary | `Monitoring.md` §vitals contract, `Wire-Protocol.md` §Control Message Variants | `product/Device-Health.md`; Wire-Protocol links |
| Rule coverage — the four states and what each means | `Monitoring.md` §System-event rules, `Rule-Administration.md` §Coverage, `API-Reference.md` §Rules, ADR-071 | `product/Alerts-and-Rules.md` defines the states; `Rule-Administration.md` describes only the screen and links; `API-Reference.md` lists the response shape |
| Alert ceilings and the rate they were sized against | `Monitoring.md` §Threshold alerts, `Rule-Administration.md` §Alert limits, ADR-076 | `product/Rule-Administration.md` (they are settings an operator moves); `Alerts-and-Rules.md` links |
| The incident model — grouping keys, windows, lifecycle | `Monitoring.md` §Incidents, `Architecture.md` §Web Client Features, `Database.md` §Investigation Tables, ADR-075 | `product/Investigations.md`; `architecture/Database.md` keeps the tables and indexes only |
| Intel AMT device model and lifecycle | `Architecture.md` §MPS, `Database.md` §AMT Devices Table, ADR-061 | `product/Intel-AMT.md` for capability, `architecture/Database.md` for the table, ADR-061 for the decision |
| Tenancy model | `Database.md` §Tenancy **and** §Multi-Tenancy (two adjacent sections on one topic in one file), `Architecture.md` §Settings, ADR-064 / ADR-062 / ADR-041 | `product/Tenancy-and-Access.md` for the model, `architecture/Database.md` for RLS mechanism |
| Session establishment | `Architecture.md` §§Session Lifecycle / Agent Session Handler / WebSocket Relay, `Wire-Protocol.md` §Handshake | `architecture/Overview.md` for the flow, `product/Remote-Sessions.md` for what the operator gets |
| ADR mutability rule | [`docs/README.md`](../../../docs/README.md) §2, [`plans-and-adrs.md`](../../rules/plans-and-adrs.md), ADR-036 | ADR-036 is the decision; `docs/README.md` and the rule file each state the one-line convention and link it |

## Live-state violations to purge

Re-verified 2026-08-19 against current line numbers.

| Location | Problem | Fix |
|---|---|---|
| [`Home.md:5-7`](../../../docs/Home.md#L5) | "The previous GitHub wiki has been removed" | Delete the note |
| [`Home.md:32`](../../../docs/Home.md#L32) | "Frozen historical ADR log" — contradicts ADR-036 and [`README.md:88-91`](../../../docs/README.md#L88), which say the combined log is mutable | "Combined ADR log (ADR-001 … ADR-012)" |
| [`README.md:5`](../../../docs/README.md#L5) | "The previous GitHub wiki … is deprecated" | Delete |
| [`README.md:11-28`](../../../docs/README.md#L11) | The whole *Why docs live in the repo* section is a post-mortem of the wiki, including a named example of SARIF export drift | Rewrite positively into two sentences on what co-locating docs with code buys, or delete |
| [`README.md:88-91`](../../../docs/README.md#L88) | "historical log from before the per-file regime" | State what it is now |
| [`README.md:140-175`](../../../docs/README.md#L140) | Diagram coverage standard names `Architecture.md` in four rows | Repoint to `architecture/Overview.md` (S1, not S6) |
| [`README.md:177-193`](../../../docs/README.md#L177) | *Directory layout* block describes a flat `*.md` tree | Rewrite to the three trees |
| [`CI-Pipeline.md:239,246,247`](../../../docs/infrastructure/CI-Pipeline.md#L239) | Three sentences comparing rulesets to "legacy branch protection", including what the legacy approach achieved | Describe the rulesets that are in place |
| [`Agent-Updates.md:79`](../../../docs/product/Agent-Updates.md#L79) | "or — historically — a workflow that auto-built agent binaries on every `feat:`/`fix:`" | Drop the clause; the live reason stands on its own |
| [`Agent-Updates.md:81`](../../../docs/product/Agent-Updates.md#L81) | "legacy manifests" | "when `sha256` is empty" — the behavior is live |
| [`Monitoring.md:5-9`](../../../docs/infrastructure/Monitoring.md#L5) | "live reconciliation on 2026-06-18 showed …" — a dated observation, and "The **intended** topology is …" | State the topology; drop the date and the hedge |
| [`Monitoring.md:406`](../../../docs/infrastructure/Monitoring.md#L406), [`Wire-Protocol.md:224`](../../../docs/architecture/Wire-Protocol.md#L224) | "the two legacy names" / "one of the legacy names" — these aliases are live | "the aliases `mem.used` and `disk.used` resolve to …" |
| [`Shell-Quality.md:67`](../../../docs/infrastructure/Shell-Quality.md#L67) | "legacy trend retirement" | Name what the test asserts |
| [`Multiscale-Readiness.md:25,116`](archive/multiscale-readiness.md#L25) | "dormant" ×2; also §3 Large-Tier Target Shape, §9 Dependency Order, §10 Open Decisions, and "future" framing in §4 | Leaves `docs/` per D3 |

**Do not over-purge.** These five lines contain banned *words* inside
descriptions of **live behavior**, and all five survive the D11 list unmatched —
which is the point of measuring the list rather than asserting it:

| Line | Live behavior it describes |
|---|---|
| [`Architecture.md:197`](../../../docs/architecture/Overview.md#L197) | "session rows the relay no longer holds" — the stale-session sweep's actual predicate |
| [`Testing.md:208`](../../../docs/infrastructure/Testing.md#L208) | "the allowed margin from the previous successful run" — the benchmark comparison baseline |
| [`Infrastructure.md:204`](../../../docs/infrastructure/OCI-Terraform.md#L204) | "used to construct the S3 endpoint" — "used to" meaning *employed to* |
| [`Monitoring.md:604`](../../../docs/infrastructure/Monitoring.md#L604) | "how many records fell off the old end" — a ring buffer's live behavior |
| [`Monitoring.md:778`](../../../docs/infrastructure/Monitoring.md#L778) | "a read that fails leaves the previous answer" standing — the gauge refresh contract |

The customer-picker sentence at
[`Architecture.md:296`](../../../docs/architecture/Overview.md#L296) ("a customer that is
deleted or retired away") is likewise live and unmatched.

## Move cost — what breaks

**Chapter inventory: 20 today.** 18 `git mv` into the three trees (3 product,
5 architecture, 10 infrastructure); `Multiscale-Readiness.md` leaves per D3;
`Architecture-Decision-Records.md` stays at root per D10. `Home.md` and
`README.md` stay and are rewritten.

**Link rewrite: ~147 occurrences**, all inside `check-doc-links` scope or the
repo root:

| Source | Occurrences | Notes |
|---|---|---|
| `docs/**` excluding `adr/` | 77 | Sibling chapters; depth changes by one |
| `docs/adr/**` | 34, across 22 files | `../Chapter.md` → `../<tree>/Chapter.md` |
| `.claude/**` excluding `plans/` | 26, across 7 files | [`techdebt.md`](../../techdebt.md), [`phases.md`](../../phases.md), [`decisions.md`](../../decisions.md), [`skills/wiki-audit/SKILL.md`](../../skills/wiki-audit/SKILL.md), and **four rule files** — [`plans-and-adrs.md`](../../rules/plans-and-adrs.md), [`coverage-exclusions.md`](../../rules/coverage-exclusions.md), [`docs-live-state.md`](../../rules/docs-live-state.md), [`editing-and-scope.md`](../../rules/editing-and-scope.md) |
| Root [`README.md`](../../../README.md) | 9 | The reference table at lines 173–181 |
| [`CLAUDE.md`](../../../CLAUDE.md) | 1 | `docs/Home.md` and `docs/README.md` pointers |

Non-Markdown references the scanner never sees and a script must catch:

- [`docs-diagrams.test.sh:66-74`](../../../scripts/tests/docs-diagrams.test.sh#L66) — seven pins by absolute path; six change, one is deleted (D13). [`:74`](../../../scripts/tests/docs-diagrams.test.sh#L74) drops 12 → 11.
- [`Makefile:156`](../../../Makefile#L156) — comment naming `docs/Infrastructure.md`.
- [`scripts/fault/pod-delete.sh:5`](../../../scripts/fault/pod-delete.sh#L5) and [`scripts/fault/bad-rollout.sh:6`](../../../scripts/fault/bad-rollout.sh#L6) — comments naming `docs/Fault-Injection.md`.
- [`check-doc-links/scan.go:66`](../../../scripts/check-doc-links/scan.go#L66) walks `docs` and `.claude` recursively, so the scanner itself needs no change.

The rewrite is scripted and verified by `check-doc-links`, not hand-edited.

## Execution — one commit, one push

S1 … S7, then B1 … B5, then C1 … C3, all staged together and committed once.

**Why A goes first.** S1 rewrites the *link targets* inside `phases.md`,
`decisions.md` and the root `README.md`; B2, B3 and C2 rewrite the *prose* of
those same files. Running them the other way round has the rewrite author
composing fresh text against pre-split paths from memory, which is exactly how a
stale `docs/Monitoring.md` gets typed into a new row.

**The consequence: `check-doc-links` runs again after C3**, not only after S1 and
S6. B and C rewrite ~240 rows and a whole page by hand, and a hand-written link
is the one thing S1's scripted rewrite cannot protect. The root `README.md` is
outside the scanner's `docs/` + `.claude/` walk, so its links are checked
separately.

**If the gauntlet fails**, fix and re-attempt. The guard runs
[`precommit-gauntlet.sh`](../../../scripts/precommit-gauntlet.sh) on every `git
commit` attempt, so a failure means no commit was created and the retry is still
the one commit D18 requires.

The step tables below are execution order, not separate deliverables.

| Step | Work | Checked by |
|---|---|---|
| **S1** | Create the three trees; `git mv` all 18 chapters (renaming `Architecture.md` → `Overview.md`, `Infrastructure.md` → `OCI-Terraform.md`, `Data-Lifecycle.md` → `Data-Erasure.md`); scripted relative-link rewrite across `docs/**`, `.claude/**`, `CLAUDE.md`, root `README.md`, `Makefile`, `scripts/fault/*.sh`. Split `Home.md` into three index tables. Repoint six diagram pins, drop the seventh, lower the total floor to 11. Rewrite `README.md` §Directory layout and the four coverage-standard rows. Route Multiscale per D3 — archive, fold its load-bearing sentence into `infrastructure/Kubernetes.md`, repoint all 14 inbound references | `check-doc-links` clean; `docs-diagrams.test.sh` green |
| **S2** | Split `Monitoring.md`. Out: 118–125 + 174–256 + 348–377 + 903–966 → `Device-Health.md`; 378–655 → `Alerts-and-Rules.md`; 656–743 → `Investigations.md`; 136–161 → `Endpoint-Logs.md`. The infrastructure chapter keeps 1–117, 126–135, 162–173, 257–347, 744–1097 | Line reconciliation |
| **S3** | Split `architecture/Overview.md`: Web Client Features, Agent Session Handler, WebRTC, Settings, Audit Log, Notifications (product half only, D9), AMT capability → product. `Overview.md` keeps the C4 views, connection model, relay, drift checks, session lifecycle, the AMT handshake, and the push mechanism | Reconciliation; the four coverage-standard diagrams it owns stay in it |
| **S4** | Semantic lift out of `Wire-Protocol.md` (alert semantics), `Database.md` (tenancy model, rule-config and investigation-table semantics), `API-Reference.md` (investigations and rules semantics). Encoding, schema, indexes and endpoint shapes stay put | Reconciliation |
| **S5** | Resolve the duplication ledger — one fact, one home, every copy replaced by a link. Nine rows, four of them new since the first draft | Each ledger row has exactly one home; grep proves it |
| **S6** | Live-state purge — the enumerated table above | The S7 live-state gate, run early, is clean |
| **S7** | Two gates + doc-of-record updates: `CLAUDE.md`, [`editing-and-scope.md`](../../rules/editing-and-scope.md), [`docs-live-state.md`](../../rules/docs-live-state.md) (point at the new gate), `wiki-audit` skill paths, and a `phases.md` Completed row | Gauntlet green |

The `phases.md` Completed row and archiving this plan happen once, at the end of
C3 — not here — because the row must describe all three workstreams, and
[`plans-archive-consistency.test.sh`](../../../scripts/tests/plans-archive-consistency.test.sh)
refuses a Completed row that links a live plan. Archiving bumps this file's own
relative links one `../` deeper **and** repoints them at the post-split tree
paths, and the new path is re-staged explicitly — `git mv` stages the pre-edit
content.

Then `/precommit` → commit → `/refactor` → push, once.

Both new scripts ship `100755`; a non-executable `scripts/tests/*.test.sh` fails
the gauntlet's shell-tests step.

## Line reconciliation (workstream A)

The one mechanical defence against silent fact loss in A, and under D18 the only
one. For each of S2, S3 and S4, before and after:

```
lines removed from the source chapter
  == lines added across the destination chapters
   - link stubs and new section headings
```

Record the arithmetic in the commit message body. A shortfall means a paragraph
was dropped, not condensed — go find it.

`Monitoring.md` is the one worth pre-computing, because it is the largest split
and its ranges are non-contiguous: 1097 lines in, roughly 660 out across four
product chapters and roughly 440 retained.

## The gates A ships (S7)

### `scripts/tests/docs-seam.test.sh`

1. Every `docs/**/*.md` outside `adr/`, `api/`, `README.md`, `Home.md` and
   `Architecture-Decision-Records.md` (D10) lives in exactly one of the three
   trees.
2. Every such chapter appears in exactly one `Home.md` index row.
3. No `docs/product/**` file links `deploy/`, `.github/`, `Makefile` or
   `scripts/` — a product chapter reaching for a deploy path means mechanism
   leaked back across the seam.

Rule 3 was measured against the current product-destined sources:
`Rule-Administration.md`, `Data-Lifecycle.md` and `Wire-Protocol.md` carry zero
such links today. `Monitoring.md`'s deploy links cluster in the sections that
stay — with two exceptions at lines 337 and 342, which is the evidence behind
D12.

### `scripts/tests/docs-live-state.test.sh`

Scope per D11: `docs/**` minus `docs/adr/**` and
`docs/Architecture-Decision-Records.md`.

Matching is **paragraph-joined, not line-based.** Markdown wraps at ~80
columns, and a line-based grep misses every phrase that straddles a wrap —
`Home.md:5-6` reads "The previous GitHub wiki has been / removed", and a
line-based `has been removed` finds nothing. Collapse each paragraph to one line
before matching, and report the first line number of the paragraph that hit.

The phrase list, measured rather than asserted:

```
is deprecated | was deprecated | has been removed | (was|were) removed
previously  | formerly  | legacy  | historically | kept for rollback | dormant
```

Three phrases from the first draft are **deliberately absent** — `used to `,
`the old `, `the previous `. Each matches ordinary live prose ("used to
construct the endpoint", "delete the old key", "the previous successful run"),
and a gate that fires on those is a gate somebody adds an allowlist to. Their
absence costs nothing: the one real violation they would have caught,
`Home.md:5-6`, is caught by `has been removed` once matching is paragraph-joined.

Measured on the tree as it stands: **12 hits, all genuine violations, zero false
positives.** Two of the twelve leave with `Multiscale-Readiness.md`; S6 fixes the
other ten. Target: zero hits with **no allowlist file** — now a reachable number
rather than an aspiration.

## Workstream B — the state files

### What each file is for

Four artefacts describe the same work today, and only one of them is durable.

| Artefact | Role | Lifetime |
|---|---|---|
| `docs/adr/ADR-NNN.md` | **The decision and its why.** Mutable, edited in place to stay true ([ADR-036](../../../docs/adr/ADR-036-mutable-adrs-current-state-doctrine.md)) | Permanent |
| [`.claude/decisions.md`](../../decisions.md) | **Index** — number → one line → status → link | Permanent |
| [`.claude/phases.md`](../../phases.md) | **Ledger** — what shipped, in what order, linking the plan and the ADRs | Permanent |
| [`.claude/techdebt.md`](../../techdebt.md) | **Register** — what is still owed, by severity | Permanent; entries leave when paid |
| `.claude/plans/archive/<plan>.md` | **The working document** an engineer followed: file inventory, steps, reviewer checklist | **Deletion-bound.** Cleaned periodically; nothing durable may link one |

The distinction that matters: an archived plan records *how the work was going to
be done*, and is discarded once it has been. An ADR records *what was decided and
why*, and is kept. `phases.md` and `decisions.md` exist to make both findable —
they are the card catalogue, not a third copy of the book.

### What is actually duplicated

Measured 2026-08-19. The method is term overlap rather than string overlap,
because paraphrase is what is happening here: distinctive terms (alphanumeric,
4+ characters, stop-words removed, link text unwrapped), asking what share of a
row's terms also occur in the document it points at.

| Comparison | Pairs | Mean term share | Above 70 % |
|---|---|---|---|
| A `decisions.md` row → its own ADR file | 63 | **85 %** | 60 |
| A `phases.md` row → the archived plan it links | 133 | **59 %** | 39 |
| A `phases.md` row → the ADR(s) it cites | 59 | **54 %** | — |
| A `techdebt.md` entry → the ADR(s) it cites | 5 of 19 | **52 %** | — |

Verbatim 8-gram overlap between a `decisions.md` row and its ADR averages **5 %**,
with no row above 25 %. That is the finding, not a reassurance: the rows say the
same things in different words. A copy can be diffed and a drift caught; a
paraphrase cannot, so the two versions of a decision separate silently and the
reader has no way to tell which one is current.

Current sizes: `phases.md` 235 KB / 159 rows, `decisions.md` 87 KB / 74 rows,
`techdebt.md` 22 KB / 19 entries — **344 KB against a 772 KB `docs/` tree.**

`techdebt.md` is the healthiest of the three and mostly stays: it is the only one
of the four whose content is *not* a retelling, because open debt has no ADR to
point at until it is paid. Only the 5 entries that cite an ADR are in scope.

### Target shape

| File | Today | After |
|---|---|---|
| `decisions.md` | 74 rows, median 783 chars, 87 KB | ≤ 200 chars of prose per row; ~15 KB |
| `phases.md` | 159 rows, median 1 550 chars, longest 4 569, 235 KB | ≤ 300 chars of prose per row; ~45 KB |
| `techdebt.md` | 19 entries, median 1 215 chars, 22 KB | 5 ADR-citing entries trimmed to the debt and its link; the rest unchanged |
| `docs/adr/**` | 63 files | Unchanged, plus whatever B1 rescues into them |

Links, ADR numbers, phase names, dates and status do not count against a cap —
only prose. A row's job is to let a reader choose a link, and 200 characters is
enough to say which decision this is and whether it still stands.

### Steps

| Step | Work | Checked by |
|---|---|---|
| **B1** | **Rescue.** For each of the 63 `decisions.md` rows and the 59 ADR-citing `phases.md` rows, diff the row's distinctive terms against the target ADR. Anything substantive the ADR lacks is written **into the ADR** — its Decision or Consequences section — before anything is cut. Record each rescue in the commit body | The B5 orphan check reports zero substantive terms unaccounted for |
| **B2** | Rewrite `decisions.md` to the index shape: `\| NNN \| one line \| phase \| status \| link \|`. Preserve every number, every `supersedes` relationship, and the header comment block | Row count unchanged at 74; every ADR file has exactly one row; caps hold |
| **B3** | Rewrite `phases.md` rows to the ledger shape: what shipped, the plan link, the ADR links. Preserve every row, its order, its Version column and its Plan column | Row count unchanged at 159; every archived plan is linked by exactly one row; caps hold |
| **B4** | Trim the 5 ADR-citing `techdebt.md` entries to the debt, its severity and its link. Leave the other 14 alone — they carry rationale that has no ADR yet | Severity ordering intact; every entry still names an owner artefact |
| **B5** | Ship `scripts/tests/state-index-density.test.sh` and update [`plans-and-adrs.md`](../../rules/plans-and-adrs.md) and [`CLAUDE.md`](../../../CLAUDE.md) with the index/ledger/register roles from D14 | Gauntlet green |

### The gate (B5)

**`scripts/tests/state-index-density.test.sh`**, `100755`:

1. No `decisions.md` row exceeds 200 characters of prose, measured with link
   targets, ADR numbers and table scaffolding removed.
2. No `phases.md` row exceeds 300 characters on the same measure.
3. Every `docs/adr/ADR-*.md` has exactly one `decisions.md` row, and every row
   resolves to a file — the index-completeness check neither file has today.
4. Every `phases.md` **Completed** row links a plan under `plans/archive/`
   (already enforced by
   [`plans-archive-consistency.test.sh`](../../../scripts/tests/plans-archive-consistency.test.sh);
   the two tests stay separate, and this one does not re-implement it).

Rule 3 is the one that earns its keep beyond the caps. Nothing checks index
completeness today, so an ADR can ship with no row and a row can outlive its
file, and neither is visible until somebody goes looking.

### Reconciliation

A's defence is line arithmetic. B's is different, because the whole
point is that lines go away: the check is **term coverage**, not line count.

```
distinctive terms in the row before
  minus terms in the row after
  minus terms present in the ADR it links
  == 0
```

A non-empty remainder is a fact that existed only in the row and is about to stop
existing anywhere. Rescue it into the ADR (B1) or keep it in the row. Run it per
file and record the residual in the commit body.

### What B does not do

- **It does not touch the archived plans.** 162 files, 1.6 MB, deletion-bound by
  design (D17). Consolidating a file that is scheduled to be deleted is wasted
  work, and lifting content *out* of one into a durable doc is how a durable doc
  acquires a dependency on something ephemeral.
- **It does not merge, renumber or supersede any ADR.** Consolidation here means
  the *state files* stop re-telling the ADRs. The ADR corpus is the destination,
  not the subject.
- **It does not touch `docs/Architecture-Decision-Records.md`** (D10), which is
  already index-dense at 12 KB for twelve decisions.

## Workstream C — the root README

### The problem with the front page

[`README.md`](../../../README.md) is 181 lines written for somebody who has already
decided to read the source. It recites mechanism — CSR enrollment, QUIC mTLS,
CIRA/APF, PTY-backed frames, uPlot, a KS distribution-shift statistic, an anomaly
bitmask, RLS, bcrypt — and a three-row stack table naming the Rust workspace, the
Go module and the React client. None of that helps a reader decide whether the
product is interesting, and all of it has a home in `docs/` after A.

It is also **out of date in the direction that matters most**. The capabilities
that shipped in the closing programme — the triage queue and the incident room,
curated detection rules with per-customer tuning and a stop switch, stall and
disk-performance vitals, maintenance mode, evidence frozen on the device and
carried inside the alert — appear nowhere on it. The page advertises the platform
as it stood before the most sellable half of it existed.

### What the page becomes

A product page. Each section answers *what can I do with this*, and links into
`docs/` for anybody who wants the mechanism.

| Section | What it says |
|---|---|
| Hero | One sentence on what OpenGate is for, and the badges (credibility — they stay) |
| The problem it solves | A technician gets 312 alerts across 40 machines at 02:41 and reads none of them. What the product does about that |
| What you can do | Six to eight capabilities, each a sentence a buyer understands: see the fleet, take a machine over in the browser, get told what broke and why, work a queue instead of an inbox, tune detection per customer, quiet a machine during host work, run several customers from one console, erase a device's data on request |
| Why it is built this way | Three claims stated as benefits, not mechanisms — devices dial out so nothing has to be exposed inbound; the device does its own analysis so the network carries summaries; an alert arrives already carrying its evidence, so nobody has to go back and ask a machine what happened an hour ago |
| Documentation | Three links — product, architecture, infrastructure — plus the API reference |

### What leaves, and where it goes

Nothing is deleted. Per D20, each fact is confirmed present in the owning chapter
before the README stops stating it.

| Leaving the README | Owning chapter after A |
|---|---|
| QUIC / mTLS / CSR enrollment, the relay, heartbeat, deregistration | `architecture/Overview.md` |
| Protocol frames, terminal PTY paths, file-transfer framing | `architecture/Wire-Protocol.md`, `product/Remote-Sessions.md` |
| CIRA / APF / WSMAN handshake detail | `architecture/Overview.md`; the capability stays in `product/Intel-AMT.md` |
| The KS statistic, anomaly bitmask, model/sampler version, per-family rates | `product/Alerts-and-Rules.md` |
| uPlot, typed arrays, the chart adapter | `product/Device-Health.md` |
| RLS, bcrypt, JWT, secret redaction, tenant context | `architecture/Database.md`, `product/Tenancy-and-Access.md` |
| The Agent / Server / Web stack table | `architecture/Overview.md` |

Two fixes on the way past, since the page is being rewritten anyway: the hero
paragraph carries a stray backtick in "Agent\`s" and the misspelling
"inteligence", and the page mixes "visualisations" with US spelling elsewhere.

### Steps

| Step | Work | Checked by |
|---|---|---|
| **C1** | **Rescue.** For every technical claim on the current README, confirm the owning chapter states it. Anything unmatched is written into that chapter first | The C3 residual check is empty |
| **C2** | Rewrite the page to the section shape above. Keep the badges and the anchor nav; drop the stack table and the mechanism tables. Keep at most one diagram, and only if it reads as a product story rather than a component graph | A reader who knows nothing about the repo can say what the product does after the first screen |
| **C3** | Repoint the Documentation section at the three trees; verify every link resolves by hand, because the root `README.md` sits outside the `check-doc-links` walk | Manual link check; `check-doc-links` green for everything it does cover |

### Reconciliation

Same shape as B's, one file:

```
distinctive terms in README before
  minus terms in README after
  minus terms present in the docs chapter that now owns them
  == 0
```

A non-empty residual is a claim the front page was making that nothing else in
the repo makes. Move it into the owning chapter (C1) or keep it on the page.

## Risks

- **Fact loss during extraction.** No gate verifies that a moved paragraph
  arrived, and a one-commit landing means no reviewable intermediate state where
  the move is separable from the edit. The line reconciliation above is the only
  defence — it is not optional. The exposure grew with the corpus: `Monitoring.md`
  now sheds ~660 lines into four chapters rather than ~580 into four.
- **This is the largest diff the repo has produced, by D18.** 18 file moves with
  no rename detection, ~147 rewritten links, six content splits, ~240
  hand-rewritten state rows and a rebuilt front page, in one commit. A stated
  cost of a requirement set with the alternative in front of it, not an
  oversight. What follows: the three reconciliation checks — A's line arithmetic,
  B's and C's term coverage — *are* the review, and their arithmetic goes in the
  commit body where a reader can check it.
- **Link-rewrite blast radius.** ~147 occurrences across 22 ADRs, 7 `.claude`
  files, the repo root, the Makefile and two fault scripts. Do the rewrite with a
  script and let `check-doc-links` prove it, rather than editing links by hand
  across a diff this size. The non-Markdown references are the ones the scanner
  cannot prove — grep for them explicitly.
- **One long gauntlet.** A single commit means a single ~13-minute run, and a
  single point of failure — a late `check-doc-links` break costs the whole run.
  Run `GO111MODULE=off go run ./scripts/check-doc-links` after S1 and again after
  S6, before invoking `/precommit`.
- **Parallel WIP collision.** The whole-tree doc-links gate walks untracked
  files, so another engineer's in-flight docs can block a commit; isolate with
  `git stash push -u -- <their paths>`.
- **`Multiscale-Readiness.md` becomes deletion-bound** once it lives under
  `plans/archive/`, which is cleaned periodically. Anything in it that is a live
  fact and load-bearing for `infrastructure/Kubernetes.md` must be folded there in
  S1, not left behind.
- **The product tree carries no diagram.** All six rows of the coverage standard
  are architecture or infrastructure by nature, so this is expected rather than a
  gap, and the standard is unchanged. Worth revisiting only if a product chapter
  later describes a flow a reader cannot follow in prose.

B carries different risks, and one of them is the serious one in this plan:

- **Consolidation is irreversible in a way the split is not.** A moves text
  between files and the reconciliation arithmetic finds a shortfall. B
  *deletes* text on the claim that an ADR already says it, and if the claim is
  wrong the fact is gone from every durable file at once. D15 exists for this,
  and the B1 rescue pass is not a formality — it is the whole safety of it. Run the term-coverage check per file and read the residual rather than
  glancing at it.
- **A paraphrase can carry a fact its source never had.** 85 % term share means
  15 % of each row is *not* in its ADR. Most of that is ordinary English, but the
  probe surfaced real identifiers (`alerts_suppressed_total`, `incidents_open`,
  `dyncfg`, `hysteresis`) sitting in a row and in no ADR. Those are exactly the
  rescues; the automated check finds candidates, a person decides which are
  substantive.
- **`phases.md` rows are load-bearing for the next agent session.** `CLAUDE.md`
  makes reading it mandatory at session start, so a row cut too hard costs
  context every session afterwards. The 300-character cap is set where a row can
  still say what shipped and why it mattered; if a row genuinely cannot, that is
  a signal it is describing a decision and belongs in an ADR, not a longer row.
- **Order inside the commit is load-bearing.** A rewrites the link targets in
  `phases.md`, `decisions.md` and the root `README.md`; B and C rewrite the prose
  of those same files. Out of order means fresh prose composed against pre-split
  paths. Hence `check-doc-links` after C3, not only after S1 and S6.
- **The front page is the one file with no gate.** The root `README.md` sits
  outside the `check-doc-links` walk over `docs/` and `.claude/`, so nothing
  catches a broken link on the most-read page in the repo. C3 checks it by hand,
  and that hand check is the whole assurance.
