# Architecture Decision Records

<!-- Index only. A decision and its why live in the ADR; a row here exists to let -->
<!-- a reader choose a link. Prose per row is capped at 200 characters, enforced -->
<!-- by scripts/tests/state-index-density.test.sh. -->
<!--   - ADR-001 … ADR-012: docs/Architecture-Decision-Records.md (combined log) -->
<!--   - ADR-013 onward:    docs/adr/ADR-NNN-title.md (one file per decision) -->
<!-- All ADRs are mutable — edit in place to keep them current (ADR-036). -->
<!-- Supersede with a new ADR only for a genuine decision change. -->
<!-- Amendment ADRs folded into their parents: 028+032 → 019; 026 → 020; 031+033 → 023. -->
<!-- See docs/README.md for the full convention. -->

| ADR | Decision | Phase | Status | Record |
|-----|----------|-------|--------|--------|
| 001 | MessagePack wire protocol, internally tagged enums, `[type][len][payload]` framing | 1 | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 002 | Golden file tests — Rust generates the fixtures, Go verifies them | 1 | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 003 | SQLite WAL via `modernc.org/sqlite`, `MaxOpenConns(1)` | 2 | Superseded by 014 | [log](../docs/Architecture-Decision-Records.md) |
| 004 | ECDSA P-256 self-signed CA, CSR enrollment at `/api/v1/enroll/{token}`, TLS 1.3 | 2 | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 005 | QUIC mTLS via quic-go; the agent opens the control stream and writes first | 4 | Accepted; rationale superseded by 037 | [log](../docs/Architecture-Decision-Records.md) |
| 006 | Platform traits with null implementations for headless and CI environments | 5 | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 007 | VAPID Web Push, keypair persisted to `{dataDir}/vapid.json` | 10 | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 008 | `aarch64-unknown-linux-musl` cross-compilation via `cross` | CD-C | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 009 | Cosign keyless signing for container images, authenticated by GitHub OIDC | CD-E | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 010 | Hardware inventory in its own `device_hardware` table, collected on demand over the control path | 12+ | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 011 | Device logs pulled on demand over the control path, one row per line, filtered in SQL | — | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 012 | SonarCloud quality gate is a hard merge block on the Clean-as-You-Code model | — | Accepted | [log](../docs/Architecture-Decision-Records.md) |
| 013 | Docs live in-repo under `/docs`; link over paraphrase | — | Accepted; immutability clause superseded by 036 | [ADR-013](../docs/adr/ADR-013-docs-in-repo-and-immutable-adrs.md) |
| 014 | PostgreSQL 17 via `pgx/v5/stdlib` with native types, deployed as the app chart's StatefulSet | 13a | Accepted (supersedes 003) | [ADR-014](../docs/adr/ADR-014-postgres-migration.md) |
| 015 | IaC defense-in-depth — Checkov, Hadolint, Trivy and gitleaks all run; one baseline is the only suppression surface | S2 | Accepted | [ADR-015](../docs/adr/ADR-015-iac-defense-in-depth.md) |
| 016 | Bidirectional goldens: Go→Rust reverse fixtures beside Rust→Go, each with a `.meta.json` sidecar | C1 | Accepted (extends 002) | [ADR-016](../docs/adr/ADR-016-bidirectional-goldens-and-sidecars.md) |
| 017 | CI gates consolidated into `ci.yml`, hard-blocking destroys on direct push | — | Accepted; trend-store clause superseded by 038 | [ADR-017](../docs/adr/ADR-017-ci-gates-consolidation.md) |
| 018 | OCI Bastion is the human node-SSH path; CI/CD uses the OKE API instead | — | Accepted | [ADR-018](../docs/adr/ADR-018-oci-bastion-operator-access.md) |
| 019 | PMAT as an augment-only quality overlay at three separately-togglable points; no existing gate replaced | — | Accepted, incl. amendments | [ADR-019](../docs/adr/ADR-019-pmat-quality-overlay.md) |
| 020 | Modular monolith — module boundaries enforced in one deployable, extraction deferred until a trigger fires | — | Accepted, incl. amendment | [ADR-020](../docs/adr/ADR-020-modular-monolith-full-hexagonal.md) |
| 021 | Per-aggregate Go repositories carved out of the monolithic `db.Store` | — | Accepted | [ADR-021](../docs/adr/ADR-021-go-per-aggregate-repositories.md) |
| 022 | Web state is per-feature: one store per feature, no global store | — | Accepted | [ADR-022](../docs/adr/ADR-022-web-per-feature-state.md) |
| 023 | Relay session-registry seam — a slim `SessionRegistry` with the in-process adapter as the only implementation | — | Accepted; distributed amendments reverted | [ADR-023](../docs/adr/ADR-023-relay-extraction-redis-session-registry.md) |
| 024 | A `ControlMessageHandler` trait around the agent's inner control fan-out, carved up per message family | — | Accepted | [ADR-024](../docs/adr/ADR-024-rust-control-message-handler-trait.md) |
| 025 | CD pre-flight digest check short-circuits a staging deploy when the target digest and `deploy/**` are both unchanged | — | Superseded by 086 | [ADR-025](../docs/adr/ADR-025-cd-preflight-digest-check.md) |
| 027 | Adversarial pen-test gate — custom Semgrep rules for the classes review keeps missing, run on the diff at commit time | — | Accepted | [ADR-027](../docs/adr/ADR-027-adversarial-pentest-precommit-gate.md) |
| 029 | Test determinism — every test runs on every machine; a dependency is provisioned, never skipped around | 13b | Accepted | [ADR-029](../docs/adr/ADR-029-test-determinism-no-silent-skips.md) |
| 030 | Kubernetes adoption — OKE plus a Helm chart as the deployment substrate | 13b | Accepted | [ADR-030](../docs/adr/ADR-030-kubernetes-adoption-oke-helm.md) |
| 034 | Shared server keys mounted read-only from the existing Kubernetes Secret, so identity survives a redeploy | 13b | Accepted; autoscaling/PDB reverted | [ADR-034](../docs/adr/ADR-034-scale-out-keda-shared-keys.md) |
| 035 | OKE free-tier block-volume remediation — the cluster sized to the 200 GB cap | 13b | Accepted | [ADR-035](../docs/adr/ADR-035-oke-free-tier-block-volume-remediation.md) |
| 036 | All ADRs are mutable current-state records; supersede only for a genuine decision change | — | Accepted (supersedes 013's immutability clause) | [ADR-036](../docs/adr/ADR-036-mutable-adrs-current-state-doctrine.md) |
| 037 | Client-first QUIC handshake and fast-path reconnect — mTLS-only auth, 1-RTT resumption adopted, 0-RTT deferred | 4 | Accepted (supersedes 005's rationale) | [ADR-037](../docs/adr/ADR-037-client-first-fast-path-reconnect.md) |
| 038 | VictoriaMetrics is the canonical numeric CI-trend store, written through one shared transport | — | Accepted (supersedes 017/019 trend clauses) | [ADR-038](../docs/adr/ADR-038-victoriametrics-ci-trend-store.md) |
| 039 | Diagrams as code, part 2 — native Mermaid C4 behind a mandatory render check, a drift guard, and a coverage standard | — | Accepted (extends DD-E) | [ADR-039](../docs/adr/ADR-039-diagrams-as-code-part-2.md) |
| 040 | Service-extraction decision lens — Balanced Coupling decides what leaves the monolith, and when | — | Accepted | [ADR-040](../docs/adr/ADR-040-service-extraction-balanced-coupling-lens.md) |
| 041 | Postgres row-level security is the tenant wall; the tenant comes from the JWT through one scoped transaction helper | ES WS-0 | Accepted | [ADR-041](../docs/adr/ADR-041-postgres-rls-multitenancy.md) |
| 042 | Control-protocol forward compatibility — tolerant unknown-message decoding, with capabilities as the primary gate | ES WS-1 | Accepted | [ADR-042](../docs/adr/ADR-042-control-forward-compat-capabilities.md) |
| 043 | The local ML sampler runs on the device, inside a bounded CPU and memory budget | ES WS-2 | Accepted | [ADR-043](../docs/adr/ADR-043-edge-sentinel-local-ml-sampler.md) |
| 044 | The server, not a scrape, writes edge telemetry to VictoriaMetrics, injecting the resolved tenant | ES WS-4 | Accepted | [ADR-044](../docs/adr/ADR-044-edge-sentinel-server-telemetry-ingest.md) |
| 045 | Load-test regression gate reads its baseline back from VictoriaMetrics and fails red | — | Accepted (supersedes 038's visibility-only clause) | [ADR-045](../docs/adr/ADR-045-load-test-regression-gate.md) |
| 046 | Raw logs are brokered on demand and never centralized — nothing is persisted server-side | ES WS-11 | Accepted | [ADR-046](../docs/adr/ADR-046-edge-sentinel-raw-log-broker.md) |
| 047 | Web telemetry charts render through a thin uPlot adapter over typed arrays, code-split into its own chunk | ES WS-6/12 | Accepted | [ADR-047](../docs/adr/ADR-047-web-telemetry-chart-engine.md) |
| 048 | The endpoint-log model is edge-stored and server-proxied; log lines stay on the machine | ES WS-13 | Accepted | [ADR-048](../docs/adr/ADR-048-edge-sentinel-endpoint-log-model.md) |
| 049 | Raw-log privacy is layered — structural controls plus redaction at the edge and again at the server | ES WS-13 | Accepted | [ADR-049](../docs/adr/ADR-049-edge-sentinel-raw-log-privacy.md) |
| 050 | Host log sources are read through their first-party CLIs, so no GPL library is linked into the agent | ES WS-13 | Accepted | [ADR-050](../docs/adr/ADR-050-edge-sentinel-log-reader-sourcing.md) |
| 051 | Local TSDB substrate chosen by bake-off: redb, on measured write throughput and crash safety | ES WS-14a | Accepted | [ADR-051](../docs/adr/ADR-051-edge-sentinel-local-tsdb-substrate.md) |
| 052 | Local TSDB build — tiered rollups behind a durable watermark, sized to a fixed on-disk cap | ES WS-14b | Accepted | [ADR-052](../docs/adr/ADR-052-edge-sentinel-local-tsdb-build.md) |
| 053 | Declarative threshold rules evaluated on the device beside the anomaly detector, with hysteresis and a sustain window | ES WS-19 | Accepted | [ADR-053](../docs/adr/ADR-053-edge-sentinel-threshold-alerts.md) |
| 054 | Right-to-be-forgotten erasure — a tombstone deny-list, a purge state machine, and a reconciliation sweep | ES WS-20 | Accepted | [ADR-054](../docs/adr/ADR-054-edge-sentinel-data-lifecycle-erasure.md) |
| 055 | No fault-injection code in the shipped binary; faults are injected from outside the process | FI1 | Accepted | [ADR-055](../docs/adr/ADR-055-fault-injection-mechanism.md) |
| 056 | Maintenance mode is a server-authoritative per-device desired state, and edge collectors are always on | — | Accepted | [ADR-056](../docs/adr/ADR-056-device-maintenance-mode.md) |
| 057 | Host metrics stream live over the existing control message; host system logs are served on demand beside them | — | Accepted | [ADR-057](../docs/adr/ADR-057-live-host-metric-streaming-and-system-logs.md) |
| 058 | Telemetry persists through a coalescing queue, and the fleet-health badge reads a bounded lookback | — | Accepted | [ADR-058](../docs/adr/ADR-058-telemetry-persist-coalescing-and-badge-lookback.md) |
| 059 | An unpaired session releases its row, and a relay-keyed sweep clears rows the relay no longer holds | — | Accepted | [ADR-059](../docs/adr/ADR-059-agent-session-row-lifecycle.md) |
| 060 | A hand-written msgpack encoder for `ControlMessage`, byte-compared against the goldens | — | Accepted | [ADR-060](../docs/adr/ADR-060-control-message-hand-written-encoder.md) |
| 061 | Intel AMT is a property of a managed device, linked by SMBIOS UUID, and a CIRA connection resolves to a tenant | — | Accepted | [ADR-061](../docs/adr/ADR-061-amt-as-device-property.md) |
| 062 | Reads are tenant-scoped and configuration is admin-gated; the fleet summary is one O(1) query | — | Accepted | [ADR-062](../docs/adr/ADR-062-tenant-scoped-reads-and-fleet-summary.md) |
| 063 | Every server-to-agent control message has a decoder, a golden and a test — completeness is asserted, not assumed | — | Accepted | [ADR-063](../docs/adr/ADR-063-server-to-agent-control-message-completeness.md) |
| 064 | Four-level tenancy — tenant, customer, site, device — resolved through one shared settings ladder | — | Accepted | [ADR-064](../docs/adr/ADR-064-four-level-tenancy-and-the-settings-ladder.md) |
| 065 | The vitals contract — a 60 s cadence, window extrema beside the averages, and a bounded dim vocabulary | EF-B2 | Accepted | [ADR-065](../docs/adr/ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md) |
| 066 | Stall vitals read straight from kernel pressure accounting; absent, never zero, where the kernel has none | EF-B4 | Accepted | [ADR-066](../docs/adr/ADR-066-stall-vitals-from-kernel-pressure.md) |
| 067 | Disk-performance vitals from per-device kernel counters, reduced worst-device per vital independently | EF-B5 | Accepted | [ADR-067](../docs/adr/ADR-067-disk-performance-vitals.md) |
| 068 | System-event rules over a polled log with a cursor, feeding one bounded per-device alert sink | EF-B6 | Accepted | [ADR-068](../docs/adr/ADR-068-system-event-rules-and-the-edge-alert-sink.md) |
| 069 | Ranking what broke moves to the device and rides the alert; the central correlation endpoint is removed | EF-B7 | Accepted | [ADR-069](../docs/adr/ADR-069-edge-correlation-ranking.md) |
| 070 | The alert-rule grammar — a closed, cost-computable shape — plus metric aliasing and explicit coverage states | EF-B8 | Accepted | [ADR-070](../docs/adr/ADR-070-rule-grammar-and-coverage.md) |
| 071 | Rule definitions are compiled-in YAML; a customer's bindings and rollout live in Postgres, and unsupported coverage is durable | EF-B9 | Accepted | [ADR-071](../docs/adr/ADR-071-rule-catalogue-bindings-and-durable-coverage.md) |
| 072 | A new rule is re-run over the device's own stored history, once per version, bounded and interruptible | EF-B10 | Accepted | [ADR-072](../docs/adr/ADR-072-retroactive-rule-evaluation.md) |
| 073 | A rule reaches an estate in stages held on quiet, with a kill switch and a ceiling the endpoint enforces itself | EF-B11 | Accepted | [ADR-073](../docs/adr/ADR-073-staged-rule-rollout-and-the-endpoint-budget.md) |
| 074 | The alert store — an idempotent identity, accounted ingest, self-contained evidence, and an erasure cascade | EF-C2 | Accepted | [ADR-074](../docs/adr/ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md) |
| 075 | Incident grouping on two axes — how wide a room is and how long it stays one — plus the lifecycle and auto-resolve | EF-C3 | Accepted | [ADR-075](../docs/adr/ADR-075-incident-grouping-lifecycle-and-auto-resolve.md) |
| 076 | Aggregate platform metrics are O(rules), carrying no entity label, and the alert rate becomes a measured gate | EF-C4 | Accepted | [ADR-076](../docs/adr/ADR-076-aggregate-platform-metrics-and-the-measured-alert-rate.md) |
| 077 | The investigations API — tenant membership is the whole gate, and the triage queue pages by keyset | EF-C5 | Accepted | [ADR-077](../docs/adr/ADR-077-investigations-api-and-the-keyset-triage-queue.md) |
| 078 | The triage workspace reads the incident snapshot and nothing else; an absence is stated, never left as a gap | EF-C6 | Accepted | [ADR-078](../docs/adr/ADR-078-the-triage-workspace-reads-a-snapshot.md) |
| 079 | Rule administration is read-for-all and write-for-admins, with labels as a cross-cutting targeting dimension | Rules admin | Accepted | [ADR-079](../docs/adr/ADR-079-rule-administration-and-the-cross-cutting-label.md) |
| 080 | One fact, one home: `docs/` splits into product, architecture and infrastructure behind a seam gate, and the state files become a capped index, ledger and register | Docs split | Accepted | [ADR-080](../docs/adr/ADR-080-one-fact-one-home-docs-and-state-files.md) |
| 081 | One composition root in `internal/app`, an acceptance tier speaking through two doors, its capability binding gated both ways, a seam between the Go tiers, and real machines in the browser | Acceptance tier | Accepted | [ADR-081](../docs/adr/ADR-081-one-composition-root-and-the-acceptance-tier.md) |
| 082 | A load run measures the system or is recorded invalid: reachable gate rows, server-side registration, a real relay, one versioned profile and bundle, no authority key off-cluster, no residue | Perf testing | Accepted | [ADR-082](../docs/adr/ADR-082-load-runs-measure-the-system-or-say-they-did-not.md) |
| 083 | Four red nightlies repaired at the fault with the test that would have caught it; registration read from the server, phases walked, fleets built through the API, production last evicted | Nightly repair | Accepted | [ADR-083](../docs/adr/ADR-083-a-nightly-that-repairs-itself-rather-than-reporting.md) |
| 084 | Staging's browser suite gets two real machines it cross-builds and enrols itself; certificate named for the in-cluster server, operator registered first, no authority key off-cluster | Staging fleet | Accepted | [ADR-084](../docs/adr/ADR-084-staging-e2e-runs-against-real-machines.md) |
| 085 | One holder at a time over the staging namespace, taken as a Lease in the cluster rather than a shared GitHub concurrency group, which a deploy awaiting its reviewer would hold for hours | Staging locking | Accepted | [ADR-085](../docs/adr/ADR-085-one-holder-at-a-time-over-staging.md) |
| 086 | The pre-flight reads what staging is running off the cluster, the deploy declares no cache its token cannot write, and a cache write we name is read back through the API | CD cache | Accepted | [ADR-086](../docs/adr/ADR-086-the-cluster-is-the-source-of-truth-for-what-is-deployed.md) |
| 087 | A run's names carry its own seed, a safety ceiling belongs to the environment that has the thing it protects, cleanup counts every kind it removes, and the local coverage guard reads the diff | Nightly repair | Accepted | [ADR-087](../docs/adr/ADR-087-a-run-is-independent-of-what-the-last-one-left.md) |
| 088 | A benchmark measures the code, not its own pipe and scheduler; each lazy engine carries its own JS budget; guards refuse a clock toggled inside the loop and a chunk subtracted without one | Gate honesty | Accepted | [ADR-088](../docs/adr/ADR-088-a-gate-measures-the-system-not-its-own-harness.md) |
| 089 | Alerts, evidence and closed rooms are swept at a year, aged on receipt so a retroactive finding survives; open work is never taken and a room outlives the alerts pointing at it | Retention | Accepted | [ADR-089](../docs/adr/ADR-089-the-declared-retention-period-is-the-one-the-tables-observe.md) |
| 090 | A run is gated on the verdict it wrote about itself rather than on its failure count; the fleet is wound down before it is read, and every workflow reads the verdict back | Run honesty | Accepted | [ADR-090](../docs/adr/ADR-090-a-run-is-gated-on-its-own-verdict-not-on-its-failure-count.md) |
| 091 | A coverage report is rewritten into the coordinates of whatever reads it and then read back; the list of what goes unmeasured is held equal wherever it is written | Rust coverage | Accepted | [ADR-091](../docs/adr/ADR-091-a-coverage-report-is-written-in-the-readers-coordinates.md) |
| 092 | A trend sample carries the workload that produced it and the window is keyed by it, so a rewritten scenario compares against itself rather than against the work it replaced | Trend identity | Accepted | [ADR-092](../docs/adr/ADR-092-a-trend-series-carries-the-workload-that-produced-it.md) |
| 093 | A relay session's lifetime belongs to the relay: the handler parks on the session's own done channel and a server-lifetime context, never on a request context a hijack left uncancellable | Relay lifetime | Accepted | [ADR-093](../docs/adr/ADR-093-a-relay-session-lifetime-is-owned-by-the-relay.md) |
| 094 | A run brackets itself with two readings of its target: replaced mid-run is invalid, not giving back what it took is failed, and both readings travel in the bundle | Run honesty | Accepted | [ADR-094](../docs/adr/ADR-094-a-run-records-what-its-target-was-holding.md) |
| 095 | The server binds a second, cluster-only listener for the exposition and pprof; the ingress routes the API port alone, and every consumer's port is read back | Two listeners | Accepted | [ADR-095](../docs/adr/ADR-095-the-server-has-two-listeners.md) |
| 096 | A counter of a resource is not a measurement of it: every liveness number read zero while the process held 7,455 goroutines, so a count is paired with a reading and the assertion is a slope | Conservation | Accepted | [ADR-096](../docs/adr/ADR-096-a-counter-of-a-resource-is-not-a-measurement-of-it.md) |
| 097 | A mutant's leash is a term of the budget: the pre-flight adds one per non-terminating mutant, costs are measured over the mutants that finish, and the coefficient is bounded by the headroom | Mutation budget | Accepted | [ADR-097](../docs/adr/ADR-097-a-mutants-leash-is-a-term-of-the-budget.md) |
| 098 | A test asserts on the code that ships, and assertion shape is not evidence of value: grading by shape inverted the correlation, so the guard refuses a copied module and an un-restored global | Test value | Accepted | [ADR-098](../docs/adr/ADR-098-a-test-asserts-on-the-code-that-ships.md) |
