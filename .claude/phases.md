# Implementation Phases

<!-- The ledger: what the system is made of, in the order it arrived. One row -->
<!-- per programme, not per change — git history holds the change log. Prose per -->
<!-- row is capped at 300 characters, enforced by -->
<!-- scripts/tests/state-index-density.test.sh. Reasoning lives in the ADRs. -->

## Completed

| Phase | Summary |
|-------|---------|
| Foundations | The Rust agent, Go server and React client: the shared protocol, the certificate authority and enrollment, the HTTP API and authentication, QUIC agent connections, platform traits, sessions and the relay, WebRTC, push notifications, Intel AMT, and the first end-to-end suite. |
| PostgreSQL | The database, its native column types and its migrations. [ADR-014](../docs/adr/ADR-014-postgresql.md). |
| Kubernetes on OKE | The cluster, the Helm chart, the HTTP edge, and the storage budget the free tier allows. Deployment reads the cluster for what is running. [ADR-030](../docs/adr/ADR-030-kubernetes-on-oke.md), [ADR-034](../docs/adr/ADR-034-server-keys-from-a-secret.md), [ADR-035](../docs/adr/ADR-035-block-volume-budget.md), [ADR-086](../docs/adr/ADR-086-deploy-reads-the-cluster.md). |
| Agent updates | Signed manifests, a push over the control channel, verification and a rollback watchdog, with the release pipeline behind it. [ADR-121](../docs/adr/ADR-121-agent-auto-update.md). |
| Module boundaries | Per-aggregate repositories in Go, a handler trait in Rust, per-feature state in the web client, and three lints holding all of it. [ADR-020](../docs/adr/ADR-020-module-boundaries.md). |
| Quality gates | SonarCloud as a merge block, 80% coverage per language, PMAT, the pen-test gate, infrastructure scanning, shell quality, and the hooks that enforce the workflow. [ADR-012](../docs/Architecture-Decision-Records.md), [ADR-015](../docs/adr/ADR-015-iac-scanning.md), [ADR-019](../docs/adr/ADR-019-pmat-quality-overlay.md), [ADR-027](../docs/adr/ADR-027-pentest-gate.md). |
| Test tiers and determinism | Every test runs on every machine; one composition root, an acceptance tier bound to the product chapters, and mutation testing as a nightly measurement. [ADR-029](../docs/adr/ADR-029-test-determinism.md), [ADR-081](../docs/adr/ADR-081-composition-root-and-test-tiers.md), [ADR-097](../docs/adr/ADR-097-mutation-budget.md). |
| Test value | A test asserts on the code that ships, and assertion shape is not evidence of value — measured, against the nightly breakage report. [ADR-098](../docs/adr/ADR-098-test-value.md). |
| CI trends | Numeric build history in VictoriaMetrics, each sample naming its workload, with the load-test gate reading its baseline back. [ADR-038](../docs/adr/ADR-038-ci-trend-store.md). |
| Fast-path reconnect | The agent opens the control stream and speaks first; a reconnect that already holds the authority hash saves a round trip, and session resumption saves the cryptography. [ADR-037](../docs/adr/ADR-037-quic-transport.md). |
| Tenancy | Four levels — tenant, customer, site, device — behind forced row-level security, with settings resolving down the ladder. [ADR-041](../docs/adr/ADR-041-postgres-row-level-security.md), [ADR-064](../docs/adr/ADR-064-tenancy.md). |
| Edge Sentinel | Detection moves to the machine: an always-on sampler, a local tiered store, catch-up after a reconnect, host discovery and inventory, endpoint logs that never leave, and erasure across every store. [ADR-043](../docs/adr/ADR-043-on-device-detection.md), [ADR-046](../docs/adr/ADR-046-logs-stay-on-the-machine.md), [ADR-052](../docs/adr/ADR-052-agent-local-store.md), [ADR-054](../docs/adr/ADR-054-data-lifecycle.md). |
| Edge-first telemetry | The vitals contract and what it costs: a reading a minute with extremes, stall and disk-performance readings from the kernel, a bounded series budget, and ingest that accounts for what it drops. [ADR-044](../docs/adr/ADR-044-telemetry-ingest.md), [ADR-065](../docs/adr/ADR-065-vitals.md). |
| Alerts and investigations | The rule engine, the alert and incident store, the metrics that watch the pack, the triage queue and the incident room, and the front door for tuning a rule. [ADR-068](../docs/adr/ADR-068-system-event-rules.md), [ADR-070](../docs/adr/ADR-070-alert-rules.md), [ADR-074](../docs/adr/ADR-074-alerts-and-incidents.md), [ADR-076](../docs/adr/ADR-076-platform-metrics.md), [ADR-077](../docs/adr/ADR-077-investigations.md), [ADR-079](../docs/adr/ADR-079-rule-administration.md). |
| Device screens | The dashboard and fleet-health cards, the device detail page, live host metrics, the system logs pane, maintenance mode, and the session row lifecycle behind them. [ADR-047](../docs/adr/ADR-047-telemetry-charts.md), [ADR-056](../docs/adr/ADR-056-maintenance-mode.md), [ADR-059](../docs/adr/ADR-059-session-row-lifecycle.md). |
| Intel AMT | Out-of-band power control as a property of the device it belongs to, joined on the firmware identifier. [ADR-061](../docs/adr/ADR-061-intel-amt.md). |
| Control protocol | A hand-written encoder producing identical bytes, tolerant decoding of what a peer has not heard of, and a proof that every server-to-agent message is complete. [ADR-042](../docs/adr/ADR-042-control-protocol-compatibility.md), [ADR-063](../docs/adr/ADR-063-control-message-encoding.md). |
| Documentation | Three trees behind a seam gate, live state as a gate rather than an aspiration, diagrams as text, and the state files as a capped index and ledger. [ADR-039](../docs/adr/ADR-039-diagrams-as-code.md), [ADR-080](../docs/adr/ADR-080-documentation.md). |
| Fault tolerance | Faults injected from outside the process — a test harness by substitution, staging-scoped tooling, and a link shaper in the machine-facing path — plus the nightly network drill. [ADR-055](../docs/adr/ADR-055-fault-injection.md). |
| Relay session lifetime | A handler parked on a hijacked request context stranded two goroutines per session until staging hit its memory limit. Fixed, and the gate classes that were blind to it closed. [ADR-093](../docs/adr/ADR-093-relay-session-lifetime.md), [ADR-095](../docs/adr/ADR-095-two-listeners.md). |
| Load testing | What makes a run valid, where a run happens, and one home for the numbers it is judged by. [ADR-082](../docs/adr/ADR-082-load-run-validity.md), [ADR-101](../docs/adr/ADR-101-load-profiles-and-limits.md), [ADR-107](../docs/adr/ADR-107-where-a-run-happens.md), [ADR-116](../docs/adr/ADR-116-forwarded-addresses.md). |
| Gate honesty | A benchmark measures the code rather than its own harness, and a coverage report is written in the coordinates of whatever reads it. [ADR-088](../docs/adr/ADR-088-benchmarks-measure-the-code.md), [ADR-091](../docs/adr/ADR-091-coverage-reports.md). |

## In Progress

| Phase | Summary |
|-------|---------|
| Seeing inside a leak | A long run keeps the target's profiles on an interval and reports what grew at a named line; a core taken off the running server says what still holds the heaviest objects. [ADR-119](../docs/adr/ADR-119-finding-a-leak.md). |

## Planned
