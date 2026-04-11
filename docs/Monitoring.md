# Monitoring & Observability

## Overview

OpenGate uses a fully self-hosted observability stack deployed alongside the application on the same VPS. Total resource usage: ~405 MB RAM (3.3% of 12 GB), ~3 GB disk.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  VPS (ARM64, 12 GB RAM, 50 GB disk)                             │
│                                                                 │
│  ┌──────────────┐    scrape /metrics    ┌───────────────────┐   │
│  │ OpenGate     │◄─────────────────────│ VictoriaMetrics   │   │
│  │ Server :8080 │   every 15s           │ :8428             │   │
│  └──────┬───────┘                       │ (metrics DB)      │   │
│         │ stdout logs                   └────────┬──────────┘   │
│         ▼                                        │              │
│  ┌──────────────┐    push logs    ┌─────────┐    │ query        │
│  │ Docker       │◄───────────────│Promtail │    │              │
│  │ /var/lib/    │  (reads JSON   │(log      │    │              │
│  │ docker/      │   container    │ shipper) │    │              │
│  │ containers/  │   logs)        └────┬─────┘    │              │
│  └──────────────┘                     │          │              │
│                                       │ push     │              │
│  ┌──────────────┐    scrape :9100     ▼          ▼              │
│  │ Node         │◄──────────────┐ ┌─────────┐ ┌────────────┐   │
│  │ Exporter     │  (VM scrapes) │ │  Loki   │ │  Grafana   │   │
│  │ (host        │               │ │  :3100  │ │  :3000     │   │
│  │  metrics)    │               │ │ (log DB)│ │ (dashboard │   │
│  └──────────────┘               │ └────┬────┘ │  + alerts) │   │
│                                 │      │      └─────┬──────┘   │
│                                 │      │ query      │          │
│                                 │      └────────────┘          │
│                                 │               │              │
│  ┌──────────────┐               │               │ alert        │
│  │ Uptime Kuma  │               │               ▼              │
│  │ :3001        │               │        ┌─────────────┐       │
│  │ (status page)│               │        │ Telegram    │       │
│  └──────────────┘               │        │ Bot API     │       │
│                                 │        └─────────────┘       │
└─────────────────────────────────────────────────────────────────┘
```

### Data Flow

| Flow | From | To | Protocol |
|------|------|----|----------|
| Metrics collection | VictoriaMetrics | OpenGate Server, Node Exporter | HTTP scrape (pull, every 15s) |
| Log collection | Promtail | Loki | HTTP push |
| Dashboard queries | Grafana | VictoriaMetrics | PromQL over HTTP |
| Log queries | Grafana | Loki | LogQL over HTTP |
| Alert notifications | Grafana | Telegram Bot API | HTTPS POST |
| Status checks | Uptime Kuma | OpenGate Server (independently) | HTTP/TCP |

### Docker Networks

Two Docker networks isolate traffic:

- **`monitoring`** (bridge) — all 6 monitoring containers communicate here
- **`app`** (`opengate_default`, external) — only VictoriaMetrics and Uptime Kuma join this network so they can reach the OpenGate server container by name (`opengate-server:8080`)

Grafana does not need the app network — it only talks to VictoriaMetrics and Loki, both on the monitoring network.

## Components

| Component | Image | RAM | Purpose |
|-----------|-------|-----|---------|
| VictoriaMetrics | `victoriametrics/victoria-metrics:v1.114.0` | ~70 MB | Prometheus-compatible metrics TSDB (PromQL/MetricsQL) |
| Grafana OSS | `grafana/grafana-oss:11.6.0` | ~120 MB | Dashboards, unified alerting |
| Loki | `grafana/loki:3.5.0` | ~100 MB | Log aggregation (LogQL) |
| Promtail | `grafana/promtail:3.5.0` | ~40 MB | Docker log collection → Loki |
| Node Exporter | `prom/node-exporter:v1.9.1` | ~15 MB | Host system metrics (CPU, RAM, disk, network) |
| Uptime Kuma | `louislam/uptime-kuma:1` | ~60 MB | Uptime monitoring, public status page |

#### Container Resource Limits

All monitoring containers have memory and CPU limits:

| Container | Memory Limit | CPU Limit |
|-----------|-------------|-----------|
| VictoriaMetrics | 512 MB | 0.5 |
| Grafana | 256 MB | 0.5 |
| Loki | 256 MB | 0.5 |
| Promtail | 128 MB | 0.25 |
| Node Exporter | 64 MB | 0.25 |
| Uptime Kuma | 256 MB | 0.25 |

### Component Details

**VictoriaMetrics** (:8428) — Time-series metrics database. Pulls (scrapes) numeric metrics from two targets every 15s: OpenGate server at `:8080/metrics` (HTTP rates, latencies, connected agents, relay sessions, DB stats, Go runtime) and Node Exporter at `:9100` (host CPU, memory, disk, network). Also scrapes itself every 30s for self-monitoring. 30-day retention.

**Grafana** (:3000) — Visualization and alerting engine. Queries VictoriaMetrics (PromQL) and Loki (LogQL) to render two provisioned dashboards (OpenGate Overview and DB Performance). Runs 6 alert rules evaluated every 1m against VictoriaMetrics data. Sends alert notifications to Telegram. Accessed via SSH tunnel only (localhost-bound, no Caddy proxy).

**Loki** (:3100) — Log aggregation database. Receives log streams pushed by Promtail and stores them with 14-day retention. Grafana queries Loki to display and search container logs. Uses TSDB schema (v13) with filesystem storage.

**Promtail** — Log shipper (no exposed port). Reads Docker container logs from `/var/lib/docker/containers/` via Docker socket service discovery. Filters to only `opengate-*` containers, parses JSON log fields (level, msg, component), and pushes structured log streams to Loki. Ingestion limit: 4 MB/s, burst 8 MB/s.

**Node Exporter** (:9100) — Host metrics exporter. Reads from `/proc`, `/sys`, and `/` (mounted read-only) and exposes OS-level metrics at `:9100/metrics` for VictoriaMetrics to scrape. Provides data for the disk usage and memory usage alert rules.

**Uptime Kuma** (:3001) — External status page. Fully independent from the rest of the monitoring stack — runs its own HTTP/TCP health checks against endpoints and provides a public status page at `status.{domain}` via Caddy reverse proxy. Does not feed into or consume data from any other monitoring component.

**Telegram Bot** — Notification delivery channel. Grafana's unified alerting sends HTTPS POST requests to the Telegram Bot API when alert rules fire or resolve. Configured manually via Grafana UI (not file-provisioned due to [Grafana bug #69950](https://github.com/grafana/grafana/issues/69950) — numeric chat IDs are unmarshaled as JSON numbers instead of strings).

## Deployment

The monitoring stack uses a separate Docker Compose file with its own project name:

```bash
# Deploy monitoring stack
docker compose --project-name opengate-monitoring \
  -f docker-compose.monitoring.yml \
  --env-file .env.monitoring \
  up -d

# Check status
docker compose --project-name opengate-monitoring \
  -f docker-compose.monitoring.yml ps
```

The monitoring compose connects to the app's Docker network (`opengate_default`) as an external network for scraping.

## Access

| Tool | Access Method | Port |
|------|---------------|------|
| Grafana | `ssh -L 3000:localhost:3000 ubuntu@<VPS>` | 3000 |
| Uptime Kuma admin | `ssh -L 3001:localhost:3001 ubuntu@<VPS>` | 3001 |
| Uptime Kuma status page | Public at `https://status.{domain}` | 443 |
| VictoriaMetrics | Internal Docker network only | 8428 |
| Loki | Internal Docker network only | 3100 |
| Node Exporter | Internal Docker network only | 9100 |

No new ports are opened in the OCI security list or UFW. All monitoring UIs are localhost-only via SSH tunnel.

## Application Instrumentation

### Structured Logging

The server outputs structured JSON logs (via `slog.JSONHandler`). Set `LOG_LEVEL=debug` for verbose output. Promtail parses JSON fields from both the server and Caddy containers.

### VictoriaMetrics Scrape Target

The server exposes a `/metrics` endpoint on port `:8080` (same router as the REST API, reachable inside the Docker network via `server:8080/metrics` — not routed by the Caddy vhost, so it is not publicly accessible). VictoriaMetrics scrapes it every 15s using the Prometheus exposition format; metrics are generated via `prometheus/client_golang` against a custom `prometheus.Registry` and all use the `opengate_` namespace.

| Metric | Type | Labels | Source |
|--------|------|--------|--------|
| `opengate_http_requests_total` | Counter | `method`, `route`, `status_code` | HTTP middleware |
| `opengate_http_request_duration_seconds` | Histogram | `method`, `route` | HTTP middleware |
| `opengate_relay_active_sessions` | Gauge | — | Relay session count |
| `opengate_agents_connected` | Gauge | — | Connected agent count |
| `opengate_mps_connected_devices` | Gauge | — | MPS device count |
| `opengate_signaling_upgrades_total` | Counter | `result` | Signaling tracker |
| `opengate_db_query_duration_seconds` | Histogram | `operation` | InstrumentedStore |
| `opengate_db_queries_total` | Counter | `operation`, `status` | InstrumentedStore |
| `opengate_db_size_bytes` | Gauge | — | SQLite PRAGMA |
| Go runtime (`go_goroutines`, `go_memstats_*`, etc.) | Various | — | Go collector |
| Process (`process_cpu_seconds_total`, `process_open_fds`) | Various | — | Process collector |

The `route` label uses chi's `RoutePattern()` (e.g., `/api/v1/devices/{id}`) to prevent cardinality explosion.

### InstrumentedStore

Every `db.Store` method call is wrapped by `metrics.InstrumentedStore`, which records query duration and count. This provides per-operation visibility (e.g., `UpsertDevice`, `ListGroups`) rather than generic SQL-level metrics.

## Grafana Dashboards

Dashboards are provisioned as code from `deploy/grafana/provisioning/dashboards/`:

| Dashboard | UID | Content |
|-----------|-----|---------|
| OpenGate Overview | `opengate-overview` | HTTP rate/latency, connected agents, relay sessions, MPS devices, signaling, goroutines, memory, DB size |
| DB Performance | `opengate-db-perf` | Query rate by operation, error rate, p50/p95/p99 duration, slowest operations, DB size trend |

## Alerting

Grafana Unified Alerting routes alerts to Telegram:

| Severity | Channel | Pending | Repeat | Examples |
|----------|---------|---------|--------|----------|
| Critical (P1) | Telegram | 1 min | 1 hour | Health check failing, disk >90% |
| Warning (P2) | Telegram | 5 min | 4 hours | p99 latency >2s, error rate >5%, disk >75%, memory >80% |

Alert rules are provisioned from `deploy/grafana/provisioning/alerting/alert-rules.yml`.

### VictoriaMetrics Recording & Alerting Rules

In addition to Grafana alerting, VictoriaMetrics evaluates its own alerting rules from `deploy/victoriametrics/alerts.yml`:

| Alert | Condition | Severity |
|-------|-----------|----------|
| ServerDown | `up{job="opengate"} == 0` for 1m | Critical |
| HighMemoryUsage | Memory usage exceeds threshold | Warning |
| HighErrorRate | Elevated HTTP error rate | Warning |
| HighP95Latency | p95 request latency above threshold | Warning |

**Contact points and notification policies** are configured manually via the Grafana UI (Alerting → Contact points, Alerting → Notification policies). File-based provisioning of the Telegram contact point is blocked by [Grafana bug #69950](https://github.com/grafana/grafana/issues/69950): numeric Telegram chat IDs are unmarshaled as JSON numbers instead of strings, causing Grafana to crash on startup. The provisioning files (`contact-points.yml`, `notification-policies.yml`) are intentionally left empty with comments explaining this.

## Data Retention

| Component | Retention | Disk Cost |
|-----------|-----------|-----------|
| VictoriaMetrics | 30 days | ~0.7 GB |
| Loki | 14 days | ~2 GB |
| Uptime Kuma | 90 days | ~50 MB |

## Configuration Files

```
deploy/
├── docker-compose.monitoring.yml    # Monitoring stack definition
├── .env.monitoring.example          # Required env vars (secrets)
├── victoriametrics/
│   ├── scrape.yml                   # Prometheus-format scrape targets
│   └── alerts.yml                   # Alerting rules (ServerDown, HighMemory, HighErrorRate, HighP95Latency)
├── loki/
│   └── loki-config.yml              # Loki single-binary config
├── promtail/
│   └── promtail-config.yml          # Docker log collection config
└── grafana/
    └── provisioning/
        ├── datasources/
        │   └── datasources.yml      # VictoriaMetrics + Loki
        ├── dashboards/
        │   ├── dashboards.yml       # Dashboard provider config
        │   ├── opengate-overview.json
        │   └── db-performance.json
        └── alerting/
            ├── alert-rules.yml
            ├── contact-points.yml
            └── notification-policies.yml
```

## Required Secrets

| Secret | Location | How to Obtain |
|--------|----------|---------------|
| `GF_SECURITY_ADMIN_PASSWORD` | `.env.monitoring` on VPS | Choose a password |
| `TELEGRAM_BOT_TOKEN` | `.env.monitoring` on VPS + GitHub Secret | Create bot via @BotFather on Telegram |
| `TELEGRAM_CHAT_ID` | `.env.monitoring` on VPS + GitHub Secret | Send message to bot, call `getUpdates` API |

## CD Integration

The monitoring stack is deployed automatically during production deployments:
1. CD workflow copies monitoring configs to VPS via `scp`
2. `deploy.sh` runs `docker compose up -d` for the monitoring project (production only)
3. Smoke tests verify the `/metrics` endpoint returns Prometheus metrics

## Uptime Kuma Monitors

Configure these monitors on first boot (via SSH tunnel to `:3001`):
- `https://<domain>/api/v1/health` — HTTP 200, 60s interval
- `https://<domain>/` — HTTP 200, 60s interval
- TCP port 9090 — QUIC agent reachability
- TCP port 4433 — MPS Intel AMT reachability

## Ad-hoc Investigation (`/observe` skill)

The `/observe` Claude Code skill (`.claude/skills/observe/SKILL.md`) automates ad-hoc queries against this stack. Neither VictoriaMetrics nor Loki publish ports on the VPS host, so queries hop through `docker exec` on the monitoring containers:

```bash
# PromQL instant query
ssh ubuntu@<VPS> "docker exec opengate-victoriametrics \
  wget -qO- 'http://127.0.0.1:8428/api/v1/query?query=<URL_ENCODED_PROMQL>'"

# LogQL range query (timestamps are nanoseconds — use `date +%s%N`)
ssh ubuntu@<VPS> "docker exec opengate-loki \
  wget -qO- 'http://127.0.0.1:3100/loki/api/v1/query_range?query=<URL_ENCODED_LOGQL>&start=<NS>&end=<NS>&limit=50'"
```

Promtail labels only scrape `opengate-*` containers — use `{container="opengate-server"}` (production) or `{container="opengate-server-staging"}` (staging). Server and Caddy logs are JSON and support `| json | ...` pipeline filters.

The skill also covers container health (`docker ps`, `docker stats`, `docker inspect`), local WSL agent diagnostics (`systemctl status mesh-agent`, `/var/log/mesh-agent/*`, cert validation via `openssl verify`), and investigation playbooks: "agent offline", "requests slow", "deployment health", "post-deploy verification".
