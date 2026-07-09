---
adr: 049
title: Edge Sentinel Raw-Log Privacy (Layered Controls + Redaction Defense-in-Depth)
status: Accepted
date: 2026-07-08
---

# ADR-049: Edge Sentinel Raw-Log Privacy (Layered Controls + Redaction Defense-in-Depth)

## Status

Accepted.

## Context

Raw endpoint log lines are the most secret-dense signal Edge-Sentinel touches —
denser than process command lines, which already default to basename + hash
([ADR-043](ADR-043-edge-sentinel-local-ml-sampler.md)). A single log line can
carry bearer/JWT session tokens, connection strings with embedded credentials,
cloud access keys, and `key=value` secrets. The on-demand raw broker
([ADR-046](ADR-046-edge-sentinel-raw-log-broker.md)) exposes those lines to an
operator, so the privacy posture for raw logs needs to be recorded explicitly.

## Decision

Protect raw logs with **layered controls**, of which redaction is the
**defense-in-depth** layer, not the primary boundary.

Primary controls (structural):

- **No central persistence.** The broker is transient — nothing raw is written
  to the control plane ([ADR-046](ADR-046-edge-sentinel-raw-log-broker.md)), so
  there is no at-rest raw-log store to leak.
- **Elevated permission.** Reading raw logs is admin-gated.
- **Audited.** Every pull writes a `device.logs.read` audit event (who, which
  device, requested window/filters) — never the returned content. The audit
  search term is recorded by length only, never echoed.
- **Bounded exposure.** Line count, per-line bytes, and the blocking-time cap
  bound how much raw text any one pull can surface.

Defense-in-depth layer:

- **Two independent redaction guards** scrub known secret shapes from every raw
  line, and neither is trusted alone. The **agent** redacts each line before it
  leaves the device (edge guard); the **server** redacts again before the line
  reaches the browser (backstop guard that runs even when agent redaction is
  off). Both guards cover the same corpus of shapes: bearer/basic auth material,
  `key=value` / `key: value` credential assignments, JWTs, AWS access-key ids,
  GCP API keys, connection strings embedding `user:pass@host`, and PEM private
  keys. The two corpora are kept in sync as paired tests, one per guard.

Redaction is a **best-effort** secret scrub, not a completeness guarantee —
secret formats are application-specific and open-ended. It reduces incidental
leakage; the structural controls above are what make raw-log access safe.

## Consequences

- A new application-specific secret format is added by extending the shared
  corpus and both guards together; the paired tests fail if the two drift apart.
- Because redaction is explicitly best-effort, the audit + elevated-permission +
  no-central-storage controls carry the privacy guarantee, and the system does
  not depend on redaction being exhaustive.
- Over-redaction is preferred to leakage: an ambiguous auth-scheme marker
  redacts the following token, so a benign value may occasionally be masked.
- The edge redaction pass runs on every raw line in a pull; its per-line cost is
  benchmarked alongside the log-rate fold in the Edge-Sentinel Criterion bench
  so the overhead is tracked before default-on.
