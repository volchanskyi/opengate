---
name: observe
description: |
  Autonomous investigation of product and infrastructure issues.
  Query metrics (PromQL), logs (LogQL), pod/container health, and local WSL
  agent diagnostics without user intervention.
---

# Observe — Autonomous Diagnostics

Investigate product and infrastructure issues by querying metrics, logs, pod
health, and local agent state. The stack runs on the **OKE cluster** (Phase 13b
cutover) — all remote queries go through `kubectl` against the cluster context;
there is no VPS to SSH into anymore.

## Prerequisites

A working kubectl context for the OKE cluster. Verify before proceeding:

```bash
kubectl config current-context     # expect the OKE context (context-c23expbbogq)
kubectl get nodes                  # worker node Ready
kubectl -n monitoring get pods     # victoriametrics / loki / grafana / promtail Running
```

Namespaces: `opengate` (prod app), `opengate-staging` (staging app),
`monitoring` (observability stack), `ingress-nginx` (edge).

---

## 1. Metrics — PromQL (VictoriaMetrics)

VictoriaMetrics is a ClusterIP service (`monitoring-victoriametrics:8428`), not
exposed outside the cluster. Query via `kubectl exec` into its pod:

### Instant query

```bash
kubectl -n monitoring exec statefulset/monitoring-victoriametrics -- \
  wget -qO- 'http://127.0.0.1:8428/api/v1/query?query=URL_ENCODED_PROMQL'
```

### Range query

```bash
kubectl -n monitoring exec statefulset/monitoring-victoriametrics -- \
  wget -qO- 'http://127.0.0.1:8428/api/v1/query_range?query=URL_ENCODED_PROMQL&start=UNIX_START&end=UNIX_END&step=60s'
```

URL-encode the query before passing it through. **If a query returns an empty
`result`, check scrape health first:** `query=up` should list every target with
value `1`. If `up` itself is empty, the scrape pipeline is down — inspect
`kubectl -n monitoring logs statefulset/monitoring-victoriametrics` (a missing
serviceaccount token there means the `kubernetes_sd` discovery can't reach the
API server).

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

**Host (Node Exporter — the OKE worker node)**

| Name | PromQL |
|------|--------|
| CPU % | `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)` |
| Memory % | `100 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes * 100)` |
| Disk % | `100 - (node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"} * 100)` |
| Scrape health | `up` |

---

## 2. Logs — LogQL (Loki)

Loki is a ClusterIP service (`monitoring-loki:3100`). Query via `kubectl exec`:

```bash
kubectl -n monitoring exec statefulset/monitoring-loki -- \
  wget -qO- 'http://127.0.0.1:3100/loki/api/v1/query_range?query=URL_ENCODED_LOGQL&start=NANO_START&end=NANO_END&limit=50'
```

Loki timestamps are **nanoseconds** (Unix epoch * 1e9). Use `date +%s%N`.

### Stream labels

Promtail discovers **pods** (not docker containers), so streams are labelled by
`namespace` / `pod` / `container` / `app`:

- `{namespace="opengate", container="server"}` — production server
- `{namespace="opengate-staging", container="server"}` — staging server
- `{namespace="opengate", container="postgres"}` — production Postgres
- `{namespace="ingress-nginx", container="controller"}` — edge proxy (replaces
  the old Caddy)

Server logs are JSON (`slog`) with fields `time`, `level`, `msg`, `method`,
`path`, `status`, `duration`.

### Key queries

| Name | LogQL |
|------|-------|
| Server errors | `{namespace="opengate", container="server"} \| json \| level="ERROR"` |
| Auth failures | `{namespace="opengate", container="server"} \| json \| msg=~".*auth.*fail.*"` |
| Slow/!2xx requests | `{namespace="opengate", container="server"} \| json \| status>=400` |
| Agent events | `{namespace="opengate", container="server"} \| json \| msg=~".*agent.*"` |
| Relay/session | `{namespace="opengate", container="server"} \| json \| msg=~".*relay.*\|.*session.*"` |
| Enrollment | `{namespace="opengate", container="server"} \| json \| msg=~".*enroll.*"` |
| Edge 5xx | `{namespace="ingress-nginx", container="controller"} \|~ " (5[0-9][0-9]) "` |

For staging, swap `namespace="opengate"` → `namespace="opengate-staging"`.

---

## 3. Pod & Container Health (kubectl)

```bash
# App pods (prod + staging) — Running, restart count, age
kubectl -n opengate get pods -o wide
kubectl -n opengate-staging get pods -o wide

# API health — production, in-cluster
kubectl -n opengate exec deploy/opengate-server -- wget -qO- http://127.0.0.1:8080/api/v1/health
# ... or externally through the ingress:
curl -fsS https://opengate.cloudisland.net/healthz

# Recent server logs (last 50 lines)
kubectl -n opengate logs deploy/opengate-server --tail 50

# Pod resource usage (needs metrics-server; else use the Host PromQL in §1)
kubectl -n opengate top pods 2>/dev/null || echo "metrics-server absent — use node-exporter PromQL"

# Running image digest (matches the deployed release?)
kubectl -n opengate get deploy/opengate-server -o jsonpath='{.spec.template.spec.containers[0].image}'
```

---

## 4. Local Agent Diagnostics (WSL)

These commands run directly on the WSL host — no remote access needed.

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
# DNS resolution — points at the OKE worker node's public IP
dig +short quic.opengate.cloudisland.net

# QUIC port reachability (UDP 9090, hostPort on the node)
timeout 3 bash -c 'echo | nc -u quic.opengate.cloudisland.net 9090 && echo "reachable" || echo "unreachable"'
```

---

## 5. Investigation Playbooks

### Why is the agent offline?

1. `systemctl status mesh-agent --no-pager` — if failed, check journal for exit reason
2. Tail agent log for errors: connection refused, cert errors, DNS failures
3. Test QUIC connectivity (UDP 9090) to the node
4. Check the server pod is Running: `kubectl -n opengate get pods`
5. Check server logs for agent-related errors in Loki (§2)

### Why are requests slow?

1. Query p95/p99 latency and slowest routes (PromQL §1)
2. Query DB latency by operation
3. Check host CPU/memory/disk utilization (node-exporter PromQL)
4. Check pod resource usage (`kubectl top pods`)
5. Correlate with error logs in Loki

### Check deployment health

1. All app pods Running, low restart count? (`kubectl get pods`)
2. Health endpoint returns 200?
3. Connected agents gauge > 0?
4. Error rate < 5%?
5. Edge (ingress-nginx) logs — any 5xx?
6. Disk and memory within thresholds?

### Post-deploy verification

1. Pod age shorter than the deploy window? (`kubectl get pods` AGE column)
2. Health endpoint OK?
3. New errors in Loki since deploy timestamp?
4. Agent reconnection — `opengate_agents_connected` gauge recovering?
5. Image digest matches expected release? (`kubectl get deploy -o jsonpath=...`)
