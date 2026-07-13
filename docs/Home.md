# OpenGate Documentation

Developer documentation for the OpenGate remote device management platform.

> **This is the canonical docs location.** The previous GitHub wiki has been
> removed. See [docs/README.md](./README.md) for documentation conventions
> (link-over-paraphrase, mutable per-file ADRs).

## Contents

| Chapter | Description |
|---------|-------------|
| [Architecture](./Architecture.md) | System overview, component interactions, connection model |
| [API Reference](./API-Reference.md) | REST API endpoints, OpenAPI spec, code generation, authentication |
| [Wire Protocol](./Wire-Protocol.md) | MessagePack framing, handshake sequence, golden file testing |
| [Platform Abstraction](./Platform-Abstraction.md) | OS-specific traits for capture, input, and service lifecycle |
| [Database](./Database.md) | PostgreSQL schema, driver, migrations, backups |
| [Data Lifecycle](./Data-Lifecycle.md) | Right-to-be-forgotten erasure — tombstone deny-list, purge state machine, reconciliation sweep |
| [Testing](./Testing.md) | Test layers, running tests, benchmarks |
| [Shell Quality](./Shell-Quality.md) | Pinned linting, formatting, execution classes, behavioral tests |
| [CI Pipeline](./CI-Pipeline.md) | Workflows, job graph, branch protection, auto-merge |
| [Continuous Deployment](./Continuous-Deployment.md) | CD pipeline, staging/production deploys, smoke tests, rollback |
| [Container Images](./Container-Images.md) | Dockerfile, GHCR registry, multi-arch builds, image tags |
| [Monitoring](./Monitoring.md) | Observability stack — VictoriaMetrics, Grafana, Loki (uptime via external SaaS) |
| [Infrastructure](./Infrastructure.md) | Terraform IaC, OKE networking, OCI Bastion, firewall, secrets management |
| [Kubernetes](./Kubernetes.md) | OKE cluster, Helm chart, ingress-nginx + cert-manager, chart validation |
| [Multiscale Readiness](./Multiscale-Readiness.md) | Scaling to >20k agents — verified current state, readiness inventory, and the architecture the Large-tier cutover entails |
| [Agent Updates](./Agent-Updates.md) | OTA update system — Ed25519 signing, rollback, GitHub Release sync |
| [Security and Dependencies](./Security-and-Dependencies.md) | CodeQL, vulnerability scanning, Dependabot, key dependencies |
| [Architecture Decision Records](./Architecture-Decision-Records.md) | Frozen historical ADR log (ADR-001 … ADR-012); current ADRs live as mutable per-file records in [`adr/`](./adr/) |
