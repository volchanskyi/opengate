# Endpoint Logs

A machine's own log is where it explains itself. OpenGate reads it **without
moving it**: the lines stay on the device, are pulled on demand, are redacted, and
are streamed straight back to the administrator who asked for them. Nothing is
stored centrally.

## Reading a machine's log

The **System Logs** pane on the device page pulls records from the host on
request.

1. Open the device page and expand **System Logs**. The first open pulls once.
2. Narrow the result with the filters: severity, time window, unit, and free-text
   search.
3. Each later pull is an explicit action — a window button, a filter change, or a
   search.

> **The machine must be online.** Logs are read from the host at the moment you
> ask, so there is nothing to return for a device that is not connected. Use the
> log lines attached to an alert's evidence to see what a machine reported before
> it went away.

The response is cached for the browser session, so returning to a device page
renders the lines already fetched instead of hitting the machine again. The caret
collapses the returned lines without discarding the filters.

Jumping from a metrics chart into the logs carries the chart's time window
straight into the log view, so you land on the same stretch you were looking at.

## Who can read logs, and what is recorded

| Control | Behaviour |
|---|---|
| Access | Reading raw logs is **administrator-elevated** |
| Audit | Every pull writes a `device.logs.read` audit event |
| Storage | Raw lines are never persisted centrally — they are brokered on demand and streamed through |
| Redaction | Applied twice, independently: the agent scrubs each line on the device, and the server scrubs again before the browser sees it |

Redaction covers the shapes secrets take in log output: authorization headers,
credential assignments, tokens, cloud keys, connection strings carrying
credentials, and private keys.

> Two independent passes are deliberate. Redaction is the control standing
> between a customer's log and a technician's screen, and a single implementation
> mistake in one of them should not be enough to leak a credential.

## The relationship to alerts

The curated system-event rules read the same journal on the device and turn
records into alerts — see [Alerts and Rules](./Alerts-and-Rules.md).

An alert can therefore carry a bounded sample of log lines as evidence. Those
lines are redacted on the device **before the alert exists**, because an alert is
the only path that lifts a log line off a host outside this pane.

## Related

- [Device Health](./Device-Health.md) — the numeric readings beside the log
- [Investigations](./Investigations.md) — where log evidence is read in context
- [Remote Sessions](./Remote-Sessions.md) — a full shell, when a log pull is not enough
