# Architecture Decision Records

<!-- Last updated: 2026-04-14 -->
<!-- Compact index. Full ADR text lives in /docs: -->
<!--   - ADR-001 through ADR-012: docs/Architecture-Decision-Records.md (frozen historical log) -->
<!--   - ADR-013 onward:          docs/adr/ADR-NNN-title.md (one immutable file per decision) -->
<!-- ADRs are immutable — never edit in place. If a decision changes, add a new ADR that supersedes the old one. -->
<!-- See docs/README.md for the full convention. -->

| ADR | Decision | Phase | Status |
|-----|----------|-------|--------|
| 001 | MessagePack wire protocol with internally tagged enums, `[type][len][payload]` framing | 1 | Accepted |
| 002 | Golden file tests — Rust generates, Go verifies | 1 | Accepted |
| 003 | SQLite WAL via `modernc.org/sqlite` (pure Go, no CGo), `MaxOpenConns(1)` | 2 | Superseded by ADR-014 |
| 004 | ECDSA P-256 self-signed CA, CSR enrollment via `/api/v1/enroll/{token}`, TLS 1.3 | 2 | Accepted |
| 005 | QUIC mTLS via quic-go — server opens control stream (workaround) | 4 | Accepted (workaround) |
| 006 | Platform traits with null impls for headless/CI environments | 5 | Accepted |
| 007 | VAPID Web Push, keypair persisted to `{dataDir}/vapid.json` | 10 | Accepted |
| 008 | `aarch64-unknown-linux-musl` cross-compilation via `cross` | CD-C | Accepted |
| 009 | Cosign keyless signing for container images (GitHub OIDC) | CD-E | Accepted |
| 010 | Separate `device_hardware` table for on-demand hardware inventory; `RestartAgent`/`RequestHardwareReport`/`HardwareReport` control messages | 12+ | Accepted |
| 011 | On-demand device logs via control path (`RequestDeviceLogs`/`DeviceLogsResponse`/`DeviceLogsError`), individual rows in `device_logs` table with 5-min cache TTL, SQL-level filtering | — | Accepted |
| 012 | SonarCloud quality gate as hard merge block — Clean-as-You-Code model, coverage/ratings/hotspots enforced on new code | — | Accepted |
| 013 | Docs live in-repo under `/docs`; wiki deprecated; link-over-paraphrase; ADRs immutable (supersede instead of edit) | — | Accepted |
| 014 | PostgreSQL 17 via `pgx/v5/stdlib`, colocated Docker container on OCI VPS, native types (`TIMESTAMPTZ`/`UUID`/`JSONB`), `pg_dump` backups, `postgres_exporter` metrics | 13a | Accepted (supersedes ADR-003) |
| 015 | IaC defense-in-depth: Checkov (terraform/dockerfile/github_actions) + Hadolint + Trivy + gitleaks all run; `.checkov.baseline` is the single suppression surface, no inline `checkov:skip` permitted | S2 | Accepted |
| 016 | Bidirectional goldens (Go→Rust reverse fixtures alongside existing Rust→Go) + `.meta.json` sidecars carrying `protocol_version`/`format` per `.bin` | C1 | Accepted (extends ADR-002) |
| 017 | CI gates consolidation: inline IaC gate into ci.yml with hard-block on direct-push destroys; drop in-repo `docs/mutation-history.jsonl` (Loki is the trend store, workflow artifact for audit); remove `environment: staging` from `load-test.yml` | — | Accepted |
| 018 | OCI Bastion (STANDARD, target = `opengate-public-subnet`, IAM-gated, 3h session TTL) replaces the static `ssh_allowed_cidr` rule for human operator SSH + monitoring-UI tunnels; CI keeps the JIT NSG-rule pattern; Cloud Agent Bastion plugin enabled on the VM; operator UX via `make tunnel` / `make ssh` with a `~/.cache/opengate` session cache | — | Accepted |
| 019 | PMAT (`pmat@3.17.0` exact pin) as augment-only quality overlay at three separately-togglable points: MCP server with 7-tool read-only allow-list, precommit `pmat tdg --threshold B+` step, nightly full-repo workflow → Loki/Grafana with ≥3-pt repo-score + single-file TDG-slip Telegram alerts; no existing gate replaced; Kaizen `--dry-run` only | — | Accepted |
