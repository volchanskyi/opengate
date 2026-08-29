# OpenGate Documentation

Developer documentation for the OpenGate remote device management platform.

> **This is the canonical docs location.** See [docs/README.md](./README.md) for
> documentation conventions (link-over-paraphrase, mutable per-file ADRs).

Three trees, one rule: a fact has one home and everything else links to it.
[`product/`](./product/) is what the system does, [`architecture/`](./architecture/)
is how it is built, [`infrastructure/`](./infrastructure/) is how it runs.

## Product — what the system does

Read in order: **Agent Deployment** gets a machine into the fleet, **Fleet and
Devices** is the day-to-day console, and the rest go deeper per capability.

| Chapter | Description |
|---------|-------------|
| [Agent Deployment](./product/Agent-Deployment.md) | Getting a machine into the fleet — requirements, enrollment tokens, the install command, what the agent runs as |
| [Fleet and Devices](./product/Fleet-and-Devices.md) | Dashboard, device list and detail, customer picker, sites, inventory, device actions, browser notifications |
| [Remote Sessions](./product/Remote-Sessions.md) | Desktop, terminal, files and chat in the browser — starting a session, which tabs appear, how the connection works |
| [Device Health](./product/Device-Health.md) | What each reading means, when one is unavailable, anomaly state, the telemetry pane, maintenance mode, offline catch-up |
| [Alerts and Rules](./product/Alerts-and-Rules.md) | Threshold and system-event detection, rule coverage, staged rollout, retroactive scan, what an alert carries |
| [Rule Administration](./product/Rule-Administration.md) | Tuning a rule, aiming it with labels, pacing its rollout, the stop switch, alert budgets |
| [Investigations](./product/Investigations.md) | How alerts group into incidents, the triage queue, the incident room, statuses, cause codes, auto-resolve |
| [Endpoint Logs](./product/Endpoint-Logs.md) | Reading a machine's own log on demand — filters, redaction, audited access |
| [Intel AMT](./product/Intel-AMT.md) | Out-of-band power actions on AMT-capable hardware, and enabling AMT on a machine |
| [Agent Updates](./product/Agent-Updates.md) | Signed over-the-air updates — publishing, pushing, verification, rollback, enrollment tokens |
| [Tenancy and Access](./product/Tenancy-and-Access.md) | Tenants, customers, sites and devices; security groups, settings screens, the audit log |
| [Data Erasure](./product/Data-Erasure.md) | Irreversible erasure of a device or a tenant — what is erased, purge stages, guarantees |

## Architecture — how it is built

| Chapter | Description |
|---------|-------------|
| [System Architecture](./architecture/System-Architecture.md) | System context and container views, connection model, relay, session lifecycle, AMT transport |
| [API Reference](./architecture/API-Reference.md) | REST API endpoints, OpenAPI spec, code generation, authentication |
| [Wire Protocol](./architecture/Wire-Protocol.md) | MessagePack framing, handshake sequence, golden file testing |
| [Database](./architecture/Database.md) | PostgreSQL schema, driver, pool, migrations, row-level security, transport security |
| [Metrics Reference](./architecture/Metrics-Reference.md) | Every Prometheus series the server publishes — names, labels, populations, and the invariants that bind them |
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
