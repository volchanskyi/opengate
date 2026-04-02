---
name: observe
description: |
  Autonomous investigation of product and infrastructure issues.
  Query metrics (PromQL), logs (LogQL), container health, and local WSL agent
  diagnostics without user intervention.
---

# Observe — Autonomous Diagnostics

Investigate product and infrastructure issues by querying metrics, logs, container health, and local agent state.

## Prerequisites

SSH connectivity to the VPS is required for remote queries. Verify before proceeding:

```bash
ssh -o ConnectTimeout=5 ubuntu@163.192.34.124 "echo ok"
```

If this fails, check `~/.ssh/config` for the `ubuntu@163.192.34.124` host alias.

Verify the monitoring stack is running:

```bash
ssh ubuntu@163.192.34.124 "docker ps --filter name=opengate- --format 'table {{.Names}}\t{{.Status}}'"
```

---

## 1. Metrics — PromQL (VictoriaMetrics)

VictoriaMetrics is not published to the VPS host. Query via `docker exec`:

### Instant query

```bash
ssh ubuntu@163.192.34.124 "docker exec opengate-victoriametrics wget -qO- 'http://127.0.0.1:8428/api/v1/query?query=URL_ENCODED_PROMQL'"
```

### Range query

```bash
ssh ubuntu@163.192.34.124 "docker exec opengate-victoriametrics wget -qO- 'http://127.0.0.1:8428/api/v1/query_range?query=URL_ENCODED_PROMQL&start=UNIX_START&end=UNIX_END&step=60s'"
```

URL-encode the query locally before passing it through SSH. Use single quotes around the full URL to avoid shell expansion.

### Key queries

**HTTP**

| Name | PromQL |
|------|--------|
| Request rate by status | `sum(rate(opengate_http_requests_total[5m])) by (status_code)` |
| Error rate % | `sum(rate(opengate_http_requests_total{status_code=~"5.."}[5m])) / sum(rate(opengate_http_requests_total[5m])) * 100` |
| p95 latency | `histogram_quantile(0.95, sum(rate(opengate_http_request_duration_seconds_bucket[5m])) by (le))` |
| Slowest routes | `topk(5, sum(rate(opengate_http_request_duration_seconds_sum[5m])) by (route) / sum(rate(opengate_http_request_duration_seconds_count[5m])) by (route))` |

**Application gauges**

| Name | PromQL |
|------|--------|
| Connected agents | `opengate_agents_connected` |
| Relay sessions | `opengate_relay_active_sessions` |
| MPS devices | `opengate_mps_connected_devices` |

**Database**

| Name | PromQL |
|------|--------|
| DB p95 latency | `histogram_quantile(0.95, sum(rate(opengate_db_query_duration_seconds_bucket[5m])) by (le, operation))` |
| DB errors | `sum(rate(opengate_db_queries_total{status="error"}[5m])) by (operation)` |
| DB size | `opengate_db_size_bytes` |

**Host (Node Exporter)**

| Name | PromQL |
|------|--------|
| CPU % | `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)` |
| Memory % | `100 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes * 100)` |
| Disk % | `100 - (node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"} * 100)` |
| Scrape health | `up` |

---

## 2. Logs — LogQL (Loki)

Loki is not published to the VPS host. Query via `docker exec`:

```bash
ssh ubuntu@163.192.34.124 "docker exec opengate-loki wget -qO- 'http://127.0.0.1:3100/loki/api/v1/query_range?query=URL_ENCODED_LOGQL&start=NANO_START&end=NANO_END&limit=50'"
```

Note: Loki timestamps are **nanoseconds** (Unix epoch * 1e9). Use `date +%s%N` for current time.

### Container labels

Promtail scrapes only `opengate-*` Docker containers:

- `{container="opengate-server"}` — production server
- `{container="opengate-server-staging"}` — staging server
- `{container="opengate-caddy"}` — production reverse proxy
- `{container="opengate-caddy-staging"}` — staging reverse proxy

Server logs are JSON (`slog`). Caddy logs are JSON (access log format).

### Key queries

| Name | LogQL |
|------|-------|
| Server errors | `{container="opengate-server"} \|= "ERROR"` |
| Auth failures | `{container="opengate-server"} \| json \| msg=~".*auth.*fail.*"` |
| Caddy 5xx | `{container="opengate-caddy"} \| json \| status >= 500` |
| Agent events | `{container="opengate-server"} \| json \| msg=~".*agent.*"` |
| Relay events | `{container="opengate-server"} \| json \| msg=~".*relay.*\|.*session.*"` |
| Enrollment | `{container="opengate-server"} \| json \| msg=~".*enroll.*"` |

For staging, replace `opengate-server` with `opengate-server-staging`.

---

## 3. Container Health (Remote via SSH)

```bash
# All container status
ssh ubuntu@163.192.34.124 "docker ps --filter name=opengate- --format 'table {{.Names}}\t{{.Status}}'"

# API health — production
ssh ubuntu@163.192.34.124 "docker exec opengate-server wget -qO- http://127.0.0.1:8080/api/v1/health"

# API health — staging
ssh ubuntu@163.192.34.124 "docker exec opengate-server-staging wget -qO- http://127.0.0.1:8080/api/v1/health"

# Container resource usage
ssh ubuntu@163.192.34.124 "docker stats --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}' \$(docker ps -q --filter name=opengate-)"

# Recent container logs (last 50 lines)
ssh ubuntu@163.192.34.124 "docker logs --tail 50 opengate-server 2>&1"

# Current image version
ssh ubuntu@163.192.34.124 "docker inspect opengate-server --format '{{.Config.Image}}'"
```

---

## 4. Local Agent Diagnostics (WSL)

These commands run directly on the WSL host — no SSH needed.

### Service and process

```bash
systemctl status mesh-agent --no-pager
journalctl -u mesh-agent --no-pager -n 50
pgrep -a mesh-agent
```

### Log files

Agent logs to `/var/log/mesh-agent/` with daily rotation (7 files retained). Format is plain text (not JSON).

```bash
ls -la /var/log/mesh-agent/
cat /var/log/mesh-agent/agent.log.*
```

### Data directory

Agent identity stored in `/var/lib/opengate-agent/`. Expected files: `device_id.txt`, `agent.crt`, `agent.key`.

```bash
ls -la /var/lib/opengate-agent/
cat /var/lib/opengate-agent/device_id.txt
```

### Certificate validation

```bash
# Agent certificate
openssl x509 -in /var/lib/opengate-agent/agent.crt -noout -dates -subject 2>/dev/null

# CA certificate
openssl x509 -in /etc/opengate-agent/ca.pem -noout -dates -subject 2>/dev/null

# Verify agent cert is signed by CA
openssl verify -CAfile /etc/opengate-agent/ca.pem /var/lib/opengate-agent/agent.crt 2>/dev/null
```

### Connectivity

```bash
# DNS resolution
dig +short quic.opengate.cloudisland.net

# QUIC port reachability (UDP 9090)
timeout 3 bash -c 'echo | nc -u quic.opengate.cloudisland.net 9090 && echo "reachable" || echo "unreachable"'
```

---

## 5. Investigation Playbooks

### Why is the agent offline?

1. `systemctl status mesh-agent --no-pager` — if failed, check journal for exit reason
2. Tail agent log for errors: connection refused, cert errors, DNS failures
3. Test QUIC connectivity (UDP 9090)
4. Check VPS server container is running
5. Check server logs for agent-related errors in Loki

### Why are requests slow?

1. Query p95/p99 latency and slowest routes (PromQL)
2. Query DB latency by operation
3. Check host CPU/memory/disk utilization
4. Check container resource usage via `docker stats`
5. Correlate with error logs in Loki

### Check deployment health

1. All `opengate-*` containers running?
2. Health endpoint returns 200?
3. Connected agents gauge > 0?
4. Error rate < 5%?
5. Caddy access logs — any 5xx?
6. Disk and memory within thresholds?

### Post-deploy verification

1. Container uptime shorter than deploy window? (`docker ps` STATUS column)
2. Health endpoint OK?
3. New errors in Loki since deploy timestamp?
4. Agent reconnection — `agents_connected` gauge recovering?
5. Image tag matches expected version? (`docker inspect`)
