# Architecture Decision Records

<!-- Index only. A decision and its reasoning live in the ADR; a row here exists -->
<!-- so a reader can choose a link. Prose per row is capped at 200 characters, -->
<!-- enforced by scripts/tests/state-index-density.test.sh. -->
<!--   - ADR-001 … ADR-012: docs/Architecture-Decision-Records.md (one page) -->
<!--   - ADR-014 onward:    docs/adr/ADR-NNN-title.md (one file each) -->
<!-- Every ADR describes live state. A decision that changes rewrites its ADR; -->
<!-- one that leaves nothing behind is deleted. Git history holds the history. -->
<!-- Numbers are not reused, so gaps are expected. -->
<!-- See docs/README.md for the conventions. -->

| ADR | Decision | Record |
|-----|----------|--------|
| 001 | MessagePack on the wire, internally tagged enums, `[type][length][payload]` framing | [log](../docs/Architecture-Decision-Records.md) |
| 002 | Golden fixtures prove Rust and Go agree, in both directions, each with a sidecar saying what it holds | [log](../docs/Architecture-Decision-Records.md) |
| 004 | The server is its own certificate authority; agents enrol by signing request over TLS 1.3 | [log](../docs/Architecture-Decision-Records.md) |
| 006 | Agent capabilities are traits, and a platform that implements none of them gets the null ones | [log](../docs/Architecture-Decision-Records.md) |
| 007 | Web push with the keypair persisted to the data directory | [log](../docs/Architecture-Decision-Records.md) |
| 008 | The agent cross-compiles to static aarch64 musl | [log](../docs/Architecture-Decision-Records.md) |
| 009 | Container images are signed without keys, through the pipeline's own identity | [log](../docs/Architecture-Decision-Records.md) |
| 010 | Hardware inventory is its own table, collected when asked for rather than on a schedule | [log](../docs/Architecture-Decision-Records.md) |
| 012 | The SonarCloud gate blocks the merge, on new code only, with per-language coverage held separately | [log](../docs/Architecture-Decision-Records.md) |
| 014 | PostgreSQL 17 through pgx, native column types, running in the cluster | [ADR-014](../docs/adr/ADR-014-postgresql.md) |
| 015 | Infrastructure code is scanned by Checkov, Hadolint and Trivy; one baseline is the only suppression | [ADR-015](../docs/adr/ADR-015-iac-scanning.md) |
| 018 | Operators reach nodes through OCI Bastion; automation uses the cluster API instead | [ADR-018](../docs/adr/ADR-018-operator-node-access.md) |
| 019 | PMAT runs at three switchable points and replaces no existing gate | [ADR-019](../docs/adr/ADR-019-pmat-quality-overlay.md) |
| 020 | Module boundaries inside one deployable, held by lints; a port is earned, and extraction needs a reason | [ADR-020](../docs/adr/ADR-020-module-boundaries.md) |
| 023 | The relay keeps its own session registry, in process, because pairing is local | [ADR-023](../docs/adr/ADR-023-relay-session-registry.md) |
| 027 | A pen-test gate runs custom rules over the diff at three points, with no inline suppression | [ADR-027](../docs/adr/ADR-027-pentest-gate.md) |
| 029 | Every test runs on every machine; a missing dependency is provisioned, never skipped around | [ADR-029](../docs/adr/ADR-029-test-determinism.md) |
| 030 | Kubernetes on OKE, packaged with Helm, with the database in the cluster | [ADR-030](../docs/adr/ADR-030-kubernetes-on-oke.md) |
| 034 | The server's four key files are mounted from a Kubernetes Secret, so identity survives a redeploy | [ADR-034](../docs/adr/ADR-034-server-keys-from-a-secret.md) |
| 035 | The cluster fits the 200 GB storage cap; the binding constraint is volume count, not size | [ADR-035](../docs/adr/ADR-035-block-volume-budget.md) |
| 037 | QUIC with mutual TLS; the agent opens the stream, and resumption carries the reconnect saving | [ADR-037](../docs/adr/ADR-037-quic-transport.md) |
| 038 | CI trends live in VictoriaMetrics; a sample names its workload and the load gate reads its baseline back | [ADR-038](../docs/adr/ADR-038-ci-trend-store.md) |
| 039 | Diagrams are Mermaid in the document, syntax-checked in CI, with pinned counts | [ADR-039](../docs/adr/ADR-039-diagrams-as-code.md) |
| 041 | Forced row-level security is the tenant wall, driven by the token through one scoped transaction | [ADR-041](../docs/adr/ADR-041-postgres-row-level-security.md) |
| 042 | An unknown control message is ignored rather than fatal, and new ones are gated on a declared capability | [ADR-042](../docs/adr/ADR-042-control-protocol-compatibility.md) |
| 043 | The device detects its own problems — an always-on sampler, plus threshold rules with hysteresis | [ADR-043](../docs/adr/ADR-043-on-device-detection.md) |
| 044 | The server writes telemetry and resolves the tenant from the connection; agents hold no store credential | [ADR-044](../docs/adr/ADR-044-telemetry-ingest.md) |
| 046 | Logs stay on the machine: brokered on demand, bounded, redacted twice, audited, never stored centrally | [ADR-046](../docs/adr/ADR-046-logs-stay-on-the-machine.md) |
| 047 | Telemetry charts run on one thin uPlot adapter, split into its own budgeted bundle | [ADR-047](../docs/adr/ADR-047-telemetry-charts.md) |
| 052 | The agent keeps its own tiered store on redb, behind a durable cursor and a disk cap | [ADR-052](../docs/adr/ADR-052-agent-local-store.md) |
| 054 | Erasure is immediate and tombstone-first; alerts and closed incidents are swept at a year, aged on receipt | [ADR-054](../docs/adr/ADR-054-data-lifecycle.md) |
| 055 | No fault code in the shipped binary; faults come from a test harness and from tooling outside the process | [ADR-055](../docs/adr/ADR-055-fault-injection.md) |
| 056 | Maintenance mode is a desired state on the device row, and collectors are otherwise always on | [ADR-056](../docs/adr/ADR-056-maintenance-mode.md) |
| 059 | A session row is released when its session ends, three ways, including a sweep for what the process cannot see | [ADR-059](../docs/adr/ADR-059-session-row-lifecycle.md) |
| 061 | Intel AMT is a property of a device, joined on the firmware identifier, which is stored and never returned | [ADR-061](../docs/adr/ADR-061-intel-amt.md) |
| 063 | Control messages encode themselves to identical bytes, and every server-to-agent message is proved complete | [ADR-063](../docs/adr/ADR-063-control-message-encoding.md) |
| 064 | Four levels of tenancy; the wall stays at the tenant, membership is the read gate, settings resolve down the ladder | [ADR-064](../docs/adr/ADR-064-tenancy.md) |
| 065 | A reading a minute with extremes beside it, 24 series per device, and absent rather than zero | [ADR-065](../docs/adr/ADR-065-vitals.md) |
| 068 | Rules for failures that cross no threshold, over a polled log with a cursor, ranked on the device | [ADR-068](../docs/adr/ADR-068-system-event-rules.md) |
| 070 | The rule engine — a closed grammar, three layers by mutability, retroactive scan, staged rollout, own budget | [ADR-070](../docs/adr/ADR-070-alert-rules.md) |
| 074 | An alert's identity is reproducible, its evidence rides with it, and incidents group by customer, scope and rule | [ADR-074](../docs/adr/ADR-074-alerts-and-incidents.md) |
| 076 | Five aggregate series about the alert pack, no entity label, every value exported including the zeros | [ADR-076](../docs/adr/ADR-076-platform-metrics.md) |
| 077 | Investigations — tenant membership is the whole gate, the queue pages by key, the room reads the snapshot | [ADR-077](../docs/adr/ADR-077-investigations.md) |
| 079 | Rules are read by every member and written by administrators; labels aim them, and a stop is a row | [ADR-079](../docs/adr/ADR-079-rule-administration.md) |
| 080 | Documentation lives in the repository in three trees, describes live state, and the ADR is a decision's only home | [ADR-080](../docs/adr/ADR-080-documentation.md) |
| 081 | One composition root, an acceptance tier bound to the product chapters, and a stated seam between tiers | [ADR-081](../docs/adr/ADR-081-composition-root-and-test-tiers.md) |
| 082 | A load run is valid, failed or invalid; every number is a reading, and the fleet is a roster the run gives back | [ADR-082](../docs/adr/ADR-082-load-run-validity.md) |
| 084 | Staging carries two real machines it builds and enrols, and one holder at a time through a Lease | [ADR-084](../docs/adr/ADR-084-staging-environment.md) |
| 086 | The deploy reads the cluster for what is running, declares no cache its token cannot write, and reads back what it does | [ADR-086](../docs/adr/ADR-086-deploy-reads-the-cluster.md) |
| 088 | A benchmark measures the code rather than its own harness, and each lazy bundle carries its own budget | [ADR-088](../docs/adr/ADR-088-benchmarks-measure-the-code.md) |
| 091 | A coverage report is rewritten into its reader's coordinates and read back; exclusions agree in all four places | [ADR-091](../docs/adr/ADR-091-coverage-reports.md) |
| 093 | The relay owns a session's lifetime, never a hijacked request context, and a slope proves the resource came back | [ADR-093](../docs/adr/ADR-093-relay-session-lifetime.md) |
| 095 | Two listeners — the API in public, the exposition and profiler cluster-only, with every consumer read back | [ADR-095](../docs/adr/ADR-095-two-listeners.md) |
| 097 | A mutant's leash is a declared term of the night's budget, and cost is measured over the mutants that finish | [ADR-097](../docs/adr/ADR-097-mutation-budget.md) |
| 098 | A test asserts on the code that ships; assertion shape is not evidence of value, and the data says so | [ADR-098](../docs/adr/ADR-098-test-value.md) |
| 101 | The profile is the only home for the numbers — one measurement, one limit, and the window it is taken over | [ADR-101](../docs/adr/ADR-101-load-profiles-and-limits.md) |
| 107 | Every profile has a venue; limits derive from the run's length, and the node is asked whether it has room | [ADR-107](../docs/adr/ADR-107-where-a-run-happens.md) |
| 116 | A forwarded address is believed only from a proxy the deployment named | [ADR-116](../docs/adr/ADR-116-forwarded-addresses.md) |
| 119 | A long run keeps the target's profiles and names the line that grew; a core says what still holds it | [ADR-119](../docs/adr/ADR-119-finding-a-leak.md) |
| 121 | Signed over-the-air agent updates, pushed by the server, verified and rolled back by the agent | [ADR-121](../docs/adr/ADR-121-agent-auto-update.md) |
