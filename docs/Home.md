# OpenGate Documentation

Developer documentation for the OpenGate remote device management platform.

> **This is the canonical docs location.** See [docs/README.md](./README.md) for
> documentation conventions (link-over-paraphrase, mutable per-file ADRs).

Three trees, one rule: a fact has one home and everything else links to it.
[`product/`](./product/) is what the system does, [`architecture/`](./architecture/)
is how it is built, [`infrastructure/`](./infrastructure/) is how it runs.

## Product — what the system does

| Chapter | Description |
|---------|-------------|
| [Fleet and Devices](./product/Fleet-and-Devices.md) | Dashboard, device list and detail, customer picker, sites, hardware inventory, the incident strip, browser notifications |
| [Remote Sessions](./product/Remote-Sessions.md) | Desktop, terminal, file transfer and chat in the browser; capability-gated tabs, WebRTC upgrade, session lifecycle |
| [Device Health](./product/Device-Health.md) | The vitals contract, per-mount disk semantics, stall and disk-performance vitals, maintenance mode, the telemetry pane, reconnect backfill |
| [Alerts and Rules](./product/Alerts-and-Rules.md) | Threshold alerts, system-event rules, rule grammar and catalogue, coverage states, staged rollout, retroactive scan, alert evidence |
| [Rule Administration](./product/Rule-Administration.md) | The operator surface for curated detection — tuning, labels, rollout pace, the stop switch, and alert budgets |
| [Investigations](./product/Investigations.md) | Incident grouping, the triage queue, the incident room, the four-status lifecycle, cause codes, auto-resolve |
| [Endpoint Logs](./product/Endpoint-Logs.md) | The System Logs pane, on-demand host log pulls, the transient broker, redaction, audited reads |
| [Intel AMT](./product/Intel-AMT.md) | Out-of-band power actions on AMT-capable devices, AMT device tracking |
| [Agent Updates](./product/Agent-Updates.md) | OTA update system — Ed25519 signing, rollback, GitHub Release sync |
| [Tenancy and Access](./product/Tenancy-and-Access.md) | Four-level tenancy, customers and sites, security groups and RBAC, user management, settings, audit log |
| [Data Erasure](./product/Data-Erasure.md) | Right-to-be-forgotten erasure — tombstone deny-list, purge state machine, reconciliation sweep |

## Architecture — how it is built

| Chapter | Description |
|---------|-------------|
| [Overview](./architecture/Overview.md) | System context and container views, connection model, relay, session lifecycle, AMT transport |
| [API Reference](./architecture/API-Reference.md) | REST API endpoints, OpenAPI spec, code generation, authentication |
| [Wire Protocol](./architecture/Wire-Protocol.md) | MessagePack framing, handshake sequence, golden file testing |
| [Database](./architecture/Database.md) | PostgreSQL schema, driver, pool, migrations, row-level security, transport security |
| [Platform Abstraction](./architecture/Platform-Abstraction.md) | OS-specific traits for capture, input, and service lifecycle |

## Infrastructure — how it runs

| Chapter | Description |
|---------|-------------|
| [Kubernetes](./infrastructure/Kubernetes.md) | OKE cluster, Helm chart, ingress-nginx + cert-manager, chart validation |
| [OCI Terraform](./infrastructure/OCI-Terraform.md) | Terraform IaC, OKE networking, OCI Bastion, firewall, secrets management |
| [CI Pipeline](./infrastructure/CI-Pipeline.md) | Workflows, job graph, rulesets, auto-merge |
| [Continuous Deployment](./infrastructure/Continuous-Deployment.md) | CD pipeline, staging/production deploys, smoke tests, rollback |
| [Container Images](./infrastructure/Container-Images.md) | Dockerfile, GHCR registry, multi-arch builds, image tags |
| [Monitoring](./infrastructure/Monitoring.md) | Observability stack — VictoriaMetrics, Grafana, Loki (uptime via external SaaS) |
| [Security and Dependencies](./infrastructure/Security-and-Dependencies.md) | CodeQL, vulnerability scanning, Dependabot, key dependencies |
| [Testing](./infrastructure/Testing.md) | Test layers, running tests, benchmarks |
| [Fault Injection](./infrastructure/Fault-Injection.md) | Fault-tolerance harness — Go adapter-substitution suite, on-demand Chaos Mesh drills, ingress edge faults, scenario SLOs |
| [Shell Quality](./infrastructure/Shell-Quality.md) | Pinned linting, formatting, execution classes, behavioral tests |

## Decision records

| Chapter | Description |
|---------|-------------|
| [Architecture Decision Records](./Architecture-Decision-Records.md) | Combined ADR log (ADR-001 … ADR-012); later ADRs live as per-file records in [`adr/`](./adr/) |
