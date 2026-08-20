# Endpoint Logs

A machine's own log is where it explains itself, and OpenGate reads it without
moving it: raw lines stay on the device and are pulled on demand, redacted, and
streamed straight back to the administrator who asked, with nothing persisted
centrally.

The curated rules that read the same journal and turn records into alerts are in
[Alerts and Rules](./Alerts-and-Rules.md).

## On-demand host log pulls

Host logs are edge-stored and server-proxied: raw lines stay on the device and
are read on demand, never centralized. The System Logs pane pulls them through
the transient broker with `source=host`, filtered by severity/time/search and an
optional unit; see [ADR-057](../adr/ADR-057-live-host-metric-streaming-and-system-logs.md).
The pane's output starts collapsed and pulls once, on its first open per device;
its filters stay live either way, and the caret collapses the returned lines
alone. The response is cached for the browser session, so returning to a device
page renders the lines it already has and every later pull is an explicit
control — a window button, a unit or severity filter, or a search.
The host log source is read through its first-party CLI (`journalctl -o json`)
rather than a GPL journal library, per
[ADR-050](../adr/ADR-050-edge-sentinel-log-reader-sourcing.md).

Raw log lines are never centralized — they are brokered on demand, redacted, and
streamed straight back to an administrator with nothing persisted; see
[ADR-046](../adr/ADR-046-edge-sentinel-raw-log-broker.md). Reading raw logs is
admin-elevated and writes a `device.logs.read` audit event on every pull. On top
of those structural controls, redaction runs as defense-in-depth through two
independent guards — the agent scrubs each line at the edge, and the server
scrubs again before the browser — over a shared corpus of secret shapes
(auth headers, credential assignments, JWTs, cloud keys, credentialed connection
strings, PEM keys); see
[ADR-049](../adr/ADR-049-edge-sentinel-raw-log-privacy.md). The broker exposes
`opengate_device_log_pulls_total` (by outcome; the `ok` series is the audited-read
count) and `opengate_device_log_pull_duration_seconds`, charted by the
Edge-Sentinel Logs dashboard.

## The logs explorer

Raw logs are read through the on-demand broker in the logs explorer
([`DeviceLogs`](../../web/src/features/devices/DeviceLogs.tsx)) with level, time-range,
and full-text filters plus level facets over the returned page, rendering only the
redacted lines the broker returns. A jump from the metrics panel carries its
window straight into the explorer.
