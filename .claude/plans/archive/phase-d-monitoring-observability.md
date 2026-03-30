# Phase D: Monitoring & Observability — Design Plan

## Context

OpenGate's CD pipeline (Phases A-C complete) deploys to a single Oracle Cloud Always Free VPS (ARM64, 2 OCPUs, 12 GB RAM, 50 GB disk). The application has **zero observability** — no metrics, no structured logging, no dashboards, no alerting beyond GitHub Issues for CI/CD failures. A production issue today requires SSHing in and running `docker logs`.

**Goal**: Full observability stack — metrics, logs, dashboards, alerting, uptime monitoring — deployed alongside the application on the same VPS.

**Constraints**:
- Oracle Cloud Always Free (ARM64 `VM.Standard.A1.Flex`)
- ~400 MB RAM used by app, ~11.5 GB available
- 50 GB disk (SQLite + Docker images + certs)
- Both staging and production on same VPS
- No public exposure of monitoring UIs (SSH-tunnel access only)

---

## Component Decisions (with alternatives evaluated)

### 1. Metrics TSDB → VictoriaMetrics (single-binary)

| Option | RAM | Disk (30d) | ARM64 | Verdict |
|--------|-----|-----------|-------|---------|
| **VictoriaMetrics** | ~70 MB | ~0.7 GB | Yes | **Selected** |
| Prometheus | ~120 MB | ~1.5 GB | Yes | Good, but 40-50% more RAM/disk |
| Grafana Mimir | ~500 MB+ | multi-GB | Yes | Overkill — designed for multi-tenant clusters |
| Grafana Cloud Free | ~30 MB (agent) | 0 | N/A | Internet dependency; can't query during outages |

**Why VictoriaMetrics**: 40-50% less RAM than Prometheus, ~3x better disk compression, full PromQL compatibility (MetricsQL is a superset), uses identical `prometheus.yml` scrape config format. Single binary, simple to deploy. On a 12 GB / 50 GB VPS, every MB matters.

**Why not Grafana Cloud**: Monitoring must work during internet outages — that's exactly when you need it most. Vendor lock-in and free tier risk are secondary concerns.

### 2. Dashboards → Grafana OSS (self-hosted)

| Option | RAM | ARM64 | IaC Support | Verdict |
|--------|-----|-------|-------------|---------|
| **Grafana OSS** | ~120 MB | Yes | Provisioning files (JSON/YAML) | **Selected** |
| Grafana Cloud Free | 0 | N/A | API/Terraform (complex) | Internet dependency |
| Netdata | ~150 MB | Yes | None | No custom dashboards, no PromQL |

**Why Grafana OSS**: Provisioned dashboards checked into git (`deploy/grafana/provisioning/`). Built-in unified alerting (no separate Alertmanager needed). SSH-tunnel access keeps it secure. 120 MB is 1% of total RAM.

### 3. Log Aggregation → Loki + Promtail

| Option | RAM | ARM64 | Grafana Integration | Verdict |
|--------|-----|-------|---------------------|---------|
| **Loki + Promtail** | ~140 MB | Yes | Native (LogQL in dashboards) | **Selected** |
| Vector + Loki | ~140 MB | Yes | Same Loki backend | Overkill transform layer |
| No log aggregation | 0 | N/A | None | `docker logs` only, no search/correlation |
| Grafana Cloud Free (50 GB/mo) | ~40 MB | N/A | Native | Internet dependency |

**Why Loki + Promtail**: Log-metric correlation in Grafana is the killer feature — seeing a latency spike in metrics and clicking through to the exact logs at that timestamp. Promtail reads Docker container logs via the Docker socket (non-intrusive, no daemon config changes). 140 MB is acceptable.

**Mandatory change**: Switch `slog.TextHandler` → `slog.JSONHandler` in `main.go:40`. Caddy already logs JSON. With both services outputting structured JSON, Promtail/Loki can parse and index fields natively.

### 4. Alerting → Grafana Alerting (built-in) + Telegram

| Option | Extra containers | Provisionable | Multi-source | Verdict |
|--------|-----------------|---------------|--------------|---------|
| **Grafana Alerting** | 0 | Yes (YAML files) | Metrics + Logs | **Selected** |
| Alertmanager | 1 (~25 MB) | Yes | Metrics only | Redundant — Grafana already does this |
| Uptime Kuma alerts | 0 | No (UI only) | Uptime only | Too limited |
| Cron + curl script | 0 | N/A | Health only | No dedup, no history, brittle |

**Why Grafana Alerting**: Zero additional containers. Alert rules provisionable as YAML (checked into git). Can alert on both VictoriaMetrics metrics AND Loki log patterns.

**Notification channels**:

| Channel | Use Case | Why |
|---------|----------|-----|
| **Telegram Bot** (primary) | Critical: server down, disk >90%, DB errors | Free, instant, mobile push, no spam risk |
| **GitHub Issues** (secondary) | Warnings: elevated latency, error rate >5% | Already implemented pattern |

**Alert severity model**:

| Severity | Channel | Pending Period | Repeat Interval | Examples |
|----------|---------|---------------|-----------------|----------|
| Critical (P1) | Telegram | 1 min | 1 hour | Health check failing, disk >90%, cert expiry <7d |
| Warning (P2) | GitHub Issue | 5 min | 4 hours | p99 latency >2s, error rate >5%, disk >75%, memory >80% |

### 5. Uptime / Status Page → Uptime Kuma

| Option | RAM | External Monitoring | Status Page | Verdict |
|--------|-----|---------------------|-------------|---------|
| **Uptime Kuma** | ~60 MB | No (same VPS) | Beautiful, public-ready | **Selected** |
| Gatus | ~25 MB | No (same VPS) | Basic | Less polished, fewer integrations |
| Better Stack Free | 0 | Yes (external) | Yes | 10 monitors, 3-min interval, vendor |
| blackbox_exporter | ~15 MB | No | No status page | No UI, internal only |

**Why Uptime Kuma**: Beautiful status page, maintenance windows (suppress alerts during deploys), 90+ notification integrations, only ~60 MB RAM. The single-point-of-failure concern (monitoring on same VPS) is mitigated by Uptime Kuma's own outgoing notifications — if Uptime Kuma itself goes down, Telegram/email heartbeat pings stop, which is detectable externally.

**Optional enhancement**: Add Better Stack free tier (10 monitors, external) as a second-phase addition. Covers the "VPS is completely unreachable" scenario that self-hosted tools can't detect.

**Monitors to configure**:
- `https://<domain>/api/v1/health` — HTTP 200, 60s interval
- `https://<domain>/` — HTTP 200, 60s interval
- TCP port 9090 — QUIC agent reachability
- TCP port 4433 — MPS Intel AMT reachability

### 6. System Metrics → Node Exporter

| Option | RAM | Scope | Verdict |
|--------|-----|-------|---------|
| **Node Exporter** | ~15 MB | Host (CPU, RAM, disk, network) | **Selected** |
| cAdvisor | ~60 MB | Per-container | Overkill for 6 containers |
| Grafana Alloy | ~100 MB | Host + containers + logs | Heavier than Node Exporter + Promtail combined |
| Netdata | ~150 MB | Everything | Too heavy, wrong ecosystem |

**Why Node Exporter**: 15 MB for complete host metrics. Pre-built Grafana dashboard "Node Exporter Full" (ID 1860). Per-container metrics from cAdvisor are unnecessary — `docker stats` suffices for ad-hoc investigation.

**Critical metrics for this VPS**:
- Disk usage (50 GB is tight)
- RAM utilization (monitoring the monitoring stack)
- CPU load (2 OCPUs, QUIC/TLS can be intensive)
- Network I/O (QUIC is UDP — useful for capacity planning)

---

## Architecture: Full Self-Hosted (Option A)

Three architectures were evaluated:

| Architecture | RAM | Disk | Internet Needed | Vendor Risk | Verdict |
|--------------|-----|------|-----------------|-------------|---------|
| **A: Full self-hosted** | ~405 MB | ~3.5 GB | No | None | **Selected** |
| B: Grafana Cloud + Alloy | ~160 MB | ~150 MB | Yes | Free tier changes | Internet dependency |
| C: Minimal (Uptime Kuma only) | ~75 MB | ~50 MB | No | None | No real observability |

**Why Option A**: 405 MB / 12,288 MB = 3.3% of total RAM. 3.5 GB / 50 GB = 7% of disk. These are rounding errors. The 245 MB RAM savings of Option B don't justify introducing internet dependency and vendor risk. Option C provides "is it up?" but not "why is it slow?".

### Resource Budget

| Component | RAM | Disk (30d) | Container |
|-----------|-----|-----------|-----------|
| VictoriaMetrics | ~70 MB | ~0.7 GB | `victoriametrics` |
| Grafana OSS | ~120 MB | ~50 MB | `grafana` |
| Loki | ~100 MB | ~2 GB | `loki` |
| Promtail | ~40 MB | negligible | `promtail` |
| Node Exporter | ~15 MB | negligible | `node-exporter` |
| Uptime Kuma | ~60 MB | ~50 MB | `uptime-kuma` |
| **Total** | **~405 MB** | **~2.8 GB** | **6 containers** |

After monitoring: ~11.1 GB RAM free, ~46 GB disk free.

---

## Application Instrumentation (Go server changes)

### 7.1 Structured Logging (mandatory, regardless of stack choice)

Change `server/cmd/meshserver/main.go:40`:
```go
// Before:
logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

// After:
level := slog.LevelInfo
if os.Getenv("LOG_LEVEL") == "debug" {
    level = slog.LevelDebug
}
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
```

This produces structured JSON that Promtail/Loki can parse natively. Add `LOG_LEVEL` to `.env.example`.

### 7.2 Prometheus Metrics Package

New package: `server/internal/metrics/`

**Dependency**: `github.com/prometheus/client_golang` (add to `server/go.mod`)

**Registry**: Custom `prometheus.Registry` (not global default) — controls exactly which metrics are exposed.

**Metrics to register**:

| Metric | Type | Labels | Source |
|--------|------|--------|--------|
| `opengate_http_requests_total` | Counter | `method`, `route`, `status_code` | HTTP middleware |
| `opengate_http_request_duration_seconds` | Histogram | `method`, `route` | HTTP middleware |
| `opengate_relay_active_sessions` | Gauge | — | `relay.ActiveSessionCount()` |
| `opengate_agents_connected` | Gauge | — | `agentapi.ConnectedAgentCount()` |
| `opengate_mps_connected_devices` | Gauge | — | `mps.ConnectedDeviceCount()` |
| `opengate_signaling_upgrades_total` | Counter | `result` (success/failure) | `signaling.SuccessCount()` / `FailureCount()` |
| `opengate_db_query_duration_seconds` | Histogram | `operation` | InstrumentedStore |
| `opengate_db_queries_total` | Counter | `operation`, `status` | InstrumentedStore |
| `opengate_db_size_bytes` | Gauge | — | Periodic PRAGMA |
| Go runtime (`go_goroutines`, `go_gc_*`, `go_memstats_*`) | Various | — | `collectors.NewGoCollector()` |
| Process (`process_cpu_seconds_total`, `process_open_fds`) | Various | — | `collectors.NewProcessCollector()` |

### 7.3 HTTP Metrics Middleware

Chi-compatible middleware in `server/internal/metrics/middleware.go`. Use `chi.RouteContext(r.Context()).RoutePattern()` for the `route` label to get templates (`/api/v1/devices/{id}`) instead of actual paths — prevents cardinality explosion.

Insert in `server/internal/api/api.go:120`, after `middleware.Recoverer`, before `SecurityHeaders`.

### 7.4 Database Instrumentation

`InstrumentedStore` wrapping `db.Store` in `server/internal/metrics/store.go`. Every `Store` method call gets timed and counted. This is superior to wrapping `*sql.DB` because Store methods have semantic meaning (`UpsertDevice`, `ListGroups`) that maps to useful labels.

Wiring in `main.go`:
```go
store, _ := db.NewSQLiteStore(...)
instrumentedStore := metrics.NewInstrumentedStore(store, registry)
// pass instrumentedStore to ServerConfig.Store
```

### 7.5 `/metrics` Endpoint

Expose on the **same HTTP server** (port 8080), NOT a separate port. Register in chi router:
```go
r.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
```

**Security**: Caddy only proxies `/api/*` and `/ws/*`. The `/metrics` path is NOT proxied → not publicly accessible. VictoriaMetrics scrapes `server:8080/metrics` over the internal Docker network.

---

## Monitoring Infrastructure

### Deployment Strategy

**Separate `deploy/docker-compose.monitoring.yml`** — different lifecycle from the application. Monitoring can be upgraded independently.

```bash
# App (existing)
docker compose --project-name opengate -f docker-compose.yml up -d

# Monitoring (new)
docker compose --project-name opengate-monitoring -f docker-compose.monitoring.yml up -d
```

The monitoring compose uses `external: true` for the app's Docker network to enable scraping.

### Access Model

| Tool | Access Method | Port |
|------|---------------|------|
| Grafana | SSH tunnel (`ssh -L 3000:localhost:3000 vps`) | 3000 |
| Uptime Kuma admin | SSH tunnel (`ssh -L 3001:localhost:3001 vps`) | 3001 |
| Uptime Kuma status page | Public via Caddy at `status.{domain}` | 443 |
| VictoriaMetrics | Internal only (Docker network) | 8428 |
| Loki | Internal only (Docker network) | 3100 |
| Node Exporter | Internal only (Docker network) | 9100 |

**No new ports in OCI security list or UFW**. All monitoring UIs are localhost-only via SSH tunnel.

### Data Retention

| Component | Retention | Disk Cost |
|-----------|-----------|-----------|
| VictoriaMetrics | 30 days | ~0.7 GB |
| Loki | 14 days | ~2 GB |
| Uptime Kuma | 90 days | ~50 MB |
| Docker logs | 10 MB × 3 (existing) | ~30 MB/container |

### Config Files (IaC, checked into git)

```
deploy/
  docker-compose.monitoring.yml
  .env.monitoring.example
  victoriametrics/
    scrape.yml                              # Prometheus-format scrape config
  loki/
    loki-config.yml                         # Single-binary Loki config
  promtail/
    promtail-config.yml                     # Docker log collection
  grafana/
    provisioning/
      datasources/
        datasources.yml                     # VictoriaMetrics + Loki
      dashboards/
        dashboards.yml                      # Dashboard provider config
        opengate-overview.json              # Application metrics
        db-performance.json                 # Database query performance
        node-exporter.json                  # System metrics (import ID 1860)
      alerting/
        alert-rules.yml                     # All alert rules
        contact-points.yml                  # Telegram + GitHub Issues
        notification-policies.yml           # Routing, grouping, repeat intervals
```

### Credential Management

| Credential | Storage | How to Obtain |
|------------|---------|---------------|
| Telegram Bot Token | `.env.monitoring` on VPS + GitHub Secret | @BotFather on Telegram |
| Telegram Chat ID | `.env.monitoring` on VPS + GitHub Secret | Send message to bot, call `getUpdates` API |
| Grafana admin password | `.env.monitoring` on VPS | `GF_SECURITY_ADMIN_PASSWORD` env var |
| Uptime Kuma password | First-run UI setup | Manual on first boot |

---

## Security

- **No public exposure**: Grafana, VictoriaMetrics, Loki, Node Exporter bind to Docker network only. No `ports:` in compose exposing them to the host.
- **Uptime Kuma**: Binds to `127.0.0.1:3001` for admin (SSH tunnel). Status page exposed publicly via Caddy at `status.{domain}` with auto-TLS. Requires DNS A record for the subdomain.
- **Grafana**: Binds to `127.0.0.1:3000`. Access via SSH tunnel only.
- **`/metrics` endpoint**: Not proxied by Caddy (only `/api/*` and `/ws/*` are). Accessible only within the Docker network.
- **Docker network isolation**: Monitoring containers join the app's default network for scraping. No internet egress needed except Uptime Kuma (for external URL checks) and Grafana (for Telegram webhook alerts).

---

## CD Integration

### Files to Modify

- `deploy/scripts/deploy.sh` — add monitoring stack deployment after app deployment
- `deploy/scripts/smoke-test.sh` — add `/metrics` endpoint check (returns 200 with Prometheus format)
- `.github/workflows/cd.yml` — copy monitoring compose + configs to VPS, deploy monitoring stack
- `deploy/.env.example` — add `LOG_LEVEL`
- `deploy/caddy/Caddyfile` — add `status.{domain}` block reverse-proxying to Uptime Kuma status page
- `deploy/cloud-init.yaml` — no changes needed (Docker already installed)
- OCI security list (Terraform) — no changes needed (no new public ports, status page reuses port 443)

### Files to Create

**Go server**:
- `server/internal/metrics/metrics.go` — registry, metric definitions, collectors
- `server/internal/metrics/middleware.go` — chi HTTP metrics middleware
- `server/internal/metrics/store.go` — InstrumentedStore wrapping db.Store

**Monitoring infrastructure**:
- `deploy/docker-compose.monitoring.yml`
- `deploy/.env.monitoring.example`
- `deploy/victoriametrics/scrape.yml`
- `deploy/loki/loki-config.yml`
- `deploy/promtail/promtail-config.yml`
- `deploy/grafana/provisioning/datasources/datasources.yml`
- `deploy/grafana/provisioning/dashboards/dashboards.yml`
- `deploy/grafana/provisioning/dashboards/opengate-overview.json`
- `deploy/grafana/provisioning/dashboards/db-performance.json`
- `deploy/grafana/provisioning/alerting/alert-rules.yml`
- `deploy/grafana/provisioning/alerting/contact-points.yml`
- `deploy/grafana/provisioning/alerting/notification-policies.yml`

**Files to modify**:
- `server/cmd/meshserver/main.go` — JSONHandler, registry, wrap store, register `/metrics`
- `server/internal/api/api.go` — add `Metrics` field to `ServerConfig`, add metrics middleware
- `server/go.mod` — add `prometheus/client_golang`

---

## Implementation Order

1. **App instrumentation** — slog JSONHandler, metrics package, `/metrics` endpoint, InstrumentedStore
2. **Monitoring compose** — VictoriaMetrics, Grafana, Loki, Promtail, Node Exporter, Uptime Kuma
3. **Grafana provisioning** — datasources, dashboards, alert rules, contact points
4. **CD integration** — deploy scripts, smoke test, workflow updates
5. **Telegram bot setup** — create bot, get chat ID, configure secrets

---

## Verification

1. `curl localhost:8080/metrics` → returns Prometheus text format with all custom metrics
2. VictoriaMetrics targets page shows all scrape targets as UP
3. Grafana dashboards at `localhost:3000` render with real data
4. Stop the server → Telegram alert fires within 2 minutes
5. Uptime Kuma at `localhost:3001` shows all monitors green
6. Grafana Explore → Loki → `{container="opengate-server"} | json | status >= 500` returns results
7. `server/internal/metrics/` has tests for middleware and InstrumentedStore

---

## Secrets to Configure After Merge

| Secret | Where | Required For |
|--------|-------|--------------|
| `TELEGRAM_BOT_TOKEN` | GitHub Secrets + VPS `.env.monitoring` | Grafana + Uptime Kuma alerts |
| `TELEGRAM_CHAT_ID` | GitHub Secrets + VPS `.env.monitoring` | Grafana + Uptime Kuma alerts |
| `GF_SECURITY_ADMIN_PASSWORD` | VPS `.env.monitoring` only | Grafana admin login |
