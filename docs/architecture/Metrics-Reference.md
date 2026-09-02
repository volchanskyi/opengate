# Metrics Reference

Every Prometheus series the OpenGate server publishes, what it counts, and the
population it counts over. Names, labels and registration live under
[`server/internal/metrics`](../../server/internal/metrics); the server exposes
them on a second HTTP listener, separate from the one that serves the REST API
and the single-page application
([`internal_listener.go`](../../server/internal/app/internal_listener.go)).
That listener also carries `net/http/pprof`. The Service publishes its port and
the Ingress routes only the API port, so both are reachable from inside the
cluster and from nowhere else — see
[Monitoring](../infrastructure/Monitoring.md).

This chapter defines the series. What an operator concludes from a reading
belongs to the capability that owns it — [Device Health](../product/Device-Health.md),
[Alerts and Rules](../product/Alerts-and-Rules.md),
[Rule Administration](../product/Rule-Administration.md),
[Investigations](../product/Investigations.md),
[Endpoint Logs](../product/Endpoint-Logs.md). How the series are scraped,
rolled up, stored, charted and retained is
[Monitoring](../infrastructure/Monitoring.md).

## Agent connections

| Series | Labels | Counts |
|---|---|---|
| `opengate_agent_tls_handshakes_total` | `resumed` | Agent QUIC connections that reached the application handshake |

The server is the only side that can report whether a TLS session resumed. An
agent's transport exposes no resumption result — quinn's `handshake_data()`
yields ALPN and SNI alone — and counting tickets taken from the agent's own
store over-counts, because rustls can take a ticket and then decline it. The
honest answer is `ConnectionState().TLS.DidResume`, read in the accept path
where the peer certificates are already being read
([`server_connection.go`](../../server/internal/agentapi/server_connection.go)).
This is where the reconnect saving in
[ADR-037](../adr/ADR-037-client-first-fast-path-reconnect.md) is measured.

The count is taken before the application handshake can fail, so the series
counts TLS handshakes rather than successful registrations, and it covers the
fast-path reconnect as well as the full one — the two branch below the
observation point. The population is connections whose control stream opened; a
connection lost earlier than that is outside the count. Both label values are
published from start-up, so the denominator below exists before the first
machine connects.

```promql
sum(rate(opengate_agent_tls_handshakes_total{resumed="true"}[1h]))
  / sum(rate(opengate_agent_tls_handshakes_total[1h]))
```

## Endpoint log pulls

| Series | Labels | Counts |
|---|---|---|
| `opengate_device_log_pulls_total` | `result` | On-demand raw-log broker pulls by outcome; the `ok` series is the audited-read count |
| `opengate_device_log_pull_duration_seconds` | `result` | Pull duration by outcome |

Raw lines stay on the device and are read on demand through the transient
broker, so these count proxied reads rather than anything centralized. Every
`ok` pull writes exactly one `device.logs.read` audit event, which is what makes
that series the audited-read count.

## Telemetry ingest

| Series | Labels | Counts |
|---|---|---|
| `opengate_edge_telemetry_ingested_total` | `type` | Telemetry control messages accepted for ingest, by control type |
| `opengate_edge_telemetry_drops_total` | `reason` | Messages discarded by a server-side bound, by typed reason |
| `opengate_edge_telemetry_clock_clamped_total` | `direction` | Agent-stamped timestamps pulled inside the accepted clock window |
| `opengate_edge_backfill_decisions_total` | `decision` | Reconnect-backfill admission decisions (`grant`, `defer`) |
| `opengate_edge_backfill_active_slots` | — | Drain slots currently granted across all agents |
| `opengate_edge_backfill_grant_rate_samples_per_second` | — | Per-slot ingest rate of the most recent grant |

The first two form a **closed ledger**: every message counted as ingested either
produces a write or files exactly one typed drop, so `ingested − drops` tracks
what was persisted. A discarded coalesced batch reports every message it
carried, so the two sides stay comparable. The invariant is pinned by
`TestTelemetryAccountingInvariant` in
[`conn_accounting_test.go`](../../server/internal/agentapi/conn_accounting_test.go),
which also fails when a new telemetry control type joins the dispatch switch
without joining the ledger.

The drop reasons cover the admission bounds (`payload_too_large`,
`interval_floor`, and their `discovery_*` counterparts), a payload carrying
nothing to store (`empty_dims`, `empty_summary`, `empty_processes`,
`empty_summaries`, `empty_discovery`), a window naming dimensions outside the
vitals contract (`unknown_dim`), the persist path (`tenant_missing`,
`persist_failed`, `persist_slots_full` — bounded per-connection persist slots
shed telemetry rather than backpressuring heartbeat, session and control), a
purged device (`tombstoned`), and reconnect backfill skipping samples older than
its own retention floor (`backfill_out_of_retention`).

`unknown_dim` moves once per window however many of that window's dimensions
were unlisted, because one window is one message and the count rides the log
line. The filter behind it is what keeps the `dim` label off agent control: an
agent free to name its own dimensions would drive central cardinality directly.
See
[ADR-065](../adr/ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md).

Alerts join the same ledger with reasons prefixed `alert_`, so a fleet-wide
rollout bug and one misbehaving device never read as the same number: the path's
own payload bound (`alert_payload_too_large`, applied before the ingest
counter), the content checks
([`conn_alerts.go`](../../server/internal/agentapi/conn_alerts.go)) for a
severity outside the closed set, an incomplete idempotency key, a rule this
build does not ship, timestamps outside the window that kind of alert is allowed
and evidence that names an unreadable codec or does not decode, plus
`alert_duplicate` for a reconnect replaying one already stored,
`alert_organization_unknown` for a machine whose customer cannot be resolved,
and `alert_organization_ceiling` for a customer's spent hourly budget.

Clock clamping is deliberately its own counter and never a drop reason: a
telemetry sample stamped outside the accepted window is pulled to the nearer
bound and still persisted, so only its timestamp changes. An alert is refused
instead — its window start is part of the identity a reconnect replay resolves
against, so pulling that to a bound would land the same alert on a different row
each time and duplicate it rather than deduplicate it. A retroactive finding is
legitimately old, so its backward bound is the wider backfill retention. The
live-path bounds sit next to the handlers in
[`conn_telemetry.go`](../../server/internal/agentapi/conn_telemetry.go);
reconnect backfill keeps its own far wider retention floor in
[`conn_backfill.go`](../../server/internal/agentapi/conn_backfill.go), so
replaying months of pre-rolled history is never truncated by the live window.

## Detection: alerts, incidents and coverage

Five aggregate series watch the rule pack itself
([`investigations.go`](../../server/internal/metrics/investigations.go)). A rule
that is valid, affordable and wrong is the one thing that can degrade every
estate at once — the closed grammar bounds what a rule can *say* and the CI cost
gate bounds what it *costs*, and neither has an opinion about whether the
numbers on it are right.

| Series | Labels | Counts |
|---|---|---|
| `opengate_alerts_created_total` | `rule_id` | Alerts that became a stored row. Replays and refusals are excluded, so a rise is new detection |
| `opengate_alerts_suppressed_total` | `reason` | Alerts that reached the server and became nothing |
| `opengate_alerts_open` | — | Alerts sitting in an incident that is not resolved |
| `opengate_incidents_open` | `status` | The triage queue, split by where each incident stands |
| `opengate_rule_coverage` | `rule_id`, `state` | Machines in each coverage state per rule, across the whole install |

**Every one is O(rules), and that is the constraint they are built around.** No
series here carries a tenant, a customer or a machine: a rule pack is a handful
of entries fixed for a release, a fleet is however many machines every customer
between them runs, and one entity label would make the platform's own monitoring
the largest cardinality source in the system it exists to watch. The `rule_id`
label is bounded by the shipped catalogue rather than by what an agent echoes
back. Every value of every closed vocabulary is exported even at zero, because a
missing series reads as "no data", which is not the same answer as "none open" —
and the two look identical exactly when somebody is checking whether a rollout
raised anything. See
[ADR-076](../adr/ADR-076-aggregate-platform-metrics-and-the-measured-alert-rate.md).

The two gauges are counts over tables that only grow, so they are refreshed on a
timer rather than computed when the endpoint is scraped, and each refresh is one
aggregate across every tenant. A read that fails leaves the previous answer
standing: a database that is briefly unreachable is not an empty triage queue.

### The alert-rate measurement

```promql
sum(increase(opengate_alerts_created_total[24h]))
  / max(sum by (rule_id) (opengate_rule_coverage))
```

The numerator is a full day of stored alerts. The denominator is the fleet, read
off the coverage gauge — its four states always sum to the fleet, so any rule's
total is the fleet size. What the resulting figure obliges, and why it needs a
real population before it means anything, is in
[ADR-076](../adr/ADR-076-aggregate-platform-metrics-and-the-measured-alert-rate.md).

## Request and session surface

| Series | Labels | Counts |
|---|---|---|
| `opengate_http_requests_total` | `method`, `route`, `status_code` | HTTP requests served |
| `opengate_http_request_duration_seconds` | `method`, `route` | HTTP request duration |
| `opengate_relay_active_sessions` | — | Relay sessions currently open |
| `opengate_agents_connected` | — | Agents currently connected |
| `opengate_mps_connected_devices` | — | Connected MPS (Intel AMT) devices |

`route` is the registered pattern rather than the requested path, so the label
stays bounded by the routing table instead of growing with traffic.

The three gauges above are maintained by the paths they describe, so each one
says its own bookkeeping ran. What says the resource came back is
[the process itself](#the-process-itself), and the two are read together.

## Audit

| Series | Labels | Counts |
|---|---|---|
| `opengate_audit_writes_total` | `result` | Audited actions, by what became of their row: `written`, `failed`, or `shed` |

An audit row is written off the response path, so a slow store never holds a
request open, and the writes hold a slot bound
([`api.go`](../../server/internal/api/api.go)) so a burst of audited actions
cannot answer a slow connection pool with more callers competing for it. `shed`
is what that bound turns away. It is counted rather than logged alone because
the three values close a ledger against the actions themselves: an audit trail
that quietly lost rows under load is one nobody can rely on, and a shed write
nothing counted is exactly that.

## Registration

| Series | Labels | Counts |
|---|---|---|
| `opengate_agent_registrations_total` | `result` | Registrations the server completed, by outcome |
| `opengate_agent_registration_duration_seconds` | `result` | Time from an accepted register frame to the device row being written and online |

Both are measured **where the device row is written**, which is the only place
the whole operation has happened. A client that stops its own clock after
handing the register frame to its transport has timed a local buffer write, so
its number reads near zero however slow the server is — which is how two
load-test ceilings on that number could never fire. Every registration is
counted separately, so a repeat registration during a reconnect storm is its own
event rather than an idempotent no-op, and the rate over the counter is
enrolments per second.

## Database

| Series | Labels | Counts |
|---|---|---|
| `opengate_db_queries_total` | `operation`, `status` | Database queries, by operation and outcome |
| `opengate_db_query_duration_seconds` | `operation` | Query duration by operation |
| `opengate_db_size_bytes` | — | Database size, from `pg_database_size` |
| `opengate_db_pool_connections` | `state` | Connection-pool occupancy: `open`, `active`, `idle`, `max` |
| `opengate_db_pool_waits_total` | — | Callers that had to wait for a connection |
| `opengate_db_pool_wait_seconds_total` | — | Total time callers spent waiting |

All four pool states are published together because no one of them answers the
question a saturation report asks: a pool with every connection checked out is
busy, and the same reading against its ceiling is exhausted. Occupancy against
`max` separates the two.

The waits are cumulative rather than instantaneous by design. The pool keeps a
running total, not a live queue length, so a gauge of "callers waiting now"
would be a number nothing measures — and the total is the stronger signal
anyway, since any increase at all says a request queued behind the pool.

The per-aggregate instrumented decorators (audit, updater, auth, device,
notifications, AMT, session) record against the same `db_query_*` pair, so they
share dashboards without duplicating label discipline.

## The process itself

| Series | Labels | Reads |
|---|---|---|
| `go_goroutines` | — | Goroutines the runtime currently holds |
| `go_memstats_*`, `go_gc_*`, `go_threads`, `go_info` | — | The Go runtime's own accounting: heap, allocation, collection, OS threads |
| `process_resident_memory_bytes` | — | Resident set size of the process |
| `process_virtual_memory_bytes`, `process_virtual_memory_max_bytes` | — | Address space in use, and the ceiling on it |
| `process_open_fds`, `process_max_fds` | — | Open file descriptors, and the ceiling on them |
| `process_cpu_seconds_total` | — | CPU time consumed |
| `process_start_time_seconds` | — | When this process started, as a Unix timestamp |

These come from the client library's `NewGoCollector` and `NewProcessCollector`
([`metrics.go`](../../server/internal/metrics/metrics.go)) rather than from
anything this codebase counts, and that is exactly what makes them worth
naming. Every `opengate_*` series above is bookkeeping some code path
maintains: it says the code that decrements a counter ran. These read the
resource. A count of live sessions returning to zero and a goroutine total that
does not are the two halves that catch a session whose teardown ran and whose
resources did not come back — the invariant
[resource conservation](../../.claude/rules/resource-conservation.md) states, and
the pairing a load run records in its own bundle
([Testing](../infrastructure/Testing.md)).

`process_start_time_seconds` answers a different question from the rest: it
changes only when the process is replaced. A reading taken at the start of a
measurement and again at the end tells whoever compares them whether both
readings came from the same process, which is what makes every other number in
between comparable.

## Chart read path

| Series | Labels | Counts |
|---|---|---|
| `opengate_metrics_grid_misalignment_total` | — | Chart samples the store returned outside the request-derived grid of the query they answered |

The read path issues its query at the grid's own instants, so this should stay
at zero; any non-zero value means the store's evaluation instants and the axis
the API publishes have diverged, which would shift every charted value into the
wrong bucket. See [API Reference](./API-Reference.md).
