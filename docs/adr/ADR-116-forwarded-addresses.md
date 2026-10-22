---
number: 116
title: A forwarded address is trusted only from a named proxy
---

# ADR-116 — A forwarded address is trusted only from a named proxy

## Context

Behind a proxy, every request arrives from the proxy's address. Anything that
counts per caller — a rate limit, an audit line — counts the proxy instead, and
believing a forwarded header from any peer lets a caller claim to be anyone.

## Decision

**The deployment names its proxies by service, and a forwarded address is
believed only from a peer it named.** From anything else, the address is the
address the connection came from.

**A signed-in request is counted under its account, not its address.**
[`AccountRateLimiter`](../../server/internal/api/ratelimit.go) keys a request
whose bearer token validates by the account it names, and one carrying a
genuine console session cookie by that session, so forty technicians behind one
office connection each keep their own allowance; a request with neither, or one
that fails validation, is counted by its address as above.

**The console signs in with a cookie no script can read.** The console's 24-hour
pass sat in page storage, was read on every request, and travelled in the live
session's address as `?auth=`, where it landed in the edge's access logs; there
was no idle sign-out, and sign-out could not end a bare pass. OWASP's session
guidance is followed instead
([`session_cookie.go`](../../server/internal/api/session_cookie.go)):

- sign-in sets `og_session` — `HttpOnly`, `SameSite=Strict`, `Path=/`, no
  `Domain`, `Secure` whenever the request reached the server over TLS — and the
  server keeps only the session id's SHA-256;
- every request that changes something carries the session's synchronizer value
  in `X-CSRF-Token`, which reaches the page only in the sign-in reply and
  `GET /auth/session` and lives in page memory, so nothing a sibling site plants
  can stand in for it; a request with a bearer header is exempt, because a
  browser never adds one by itself;
- the server signs a session out after 30 minutes idle or 8 hours in all, and
  sign-out deletes its row; the console warns two minutes before the idle limit;
- the relay takes the cookie or a bearer header and refuses an `Origin` other
  than the console's own — the lesson of MeshCentral's CVE-2024-26135 — and
  re-checks the session behind each open browser side every 30 minutes, closing
  it once that session was signed out or passed its 8 hours. Console idleness
  alone never closes it, so a long repair is not cut off.

The bearer login stays for scripts, load runs and the agent side of the relay.

**The address a machine dials for a session is a setting, never read from a
request.** The server sends a host the relay address in
`OPENGATE_AGENT_RELAY_URL` (or `-agent-relay-url`), which it refuses to start
without, and builds the browser's address from the browser's own request, so no
header a browser sends decides where a host connects. Helm defaults the setting
to the release's own in-cluster service; the compose stacks name the server's
container.

**A load run presents one address per technician and one per machine**, and
signs each simulated technician in to an account of its own, so a per-caller
limit is measured per caller rather than being hit at once by one generator
pretending to be a fleet.

**A run counts how much of what it asked was refused, and carries the count into
its own evidence.** The chain that makes a presented address believed runs
through five files and a cluster, and only the files half is checkable as text:
whether the generator pods became endpoints of the named service in time, and
whether the resolver answered for them, is a fact about the night. A night where
they did not is a night spent behind one allowance — it fills with refusals,
reds the error-rate gate, and is shaped exactly like a night against a slow
server. What k6 publishes about failures is a single pass/fail rate with no
breakdown by status, so the count is taken by the scenarios themselves, through
the one request client they all share, and read back beside the run's verdict.

## Consequences

Adding a proxy means naming it in the deployment. An unnamed proxy is not
trusted, and the run says so in a number rather than in the shape of a slow
night: a broken test setup and a product regression stop looking the same.
