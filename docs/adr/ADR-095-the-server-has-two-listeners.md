---
number: 95
title: The server has two listeners
status: Accepted
date: 2026-09-02
---

# ADR-095 — The server has two listeners

## Context

Two comments in this repository asserted that the Prometheus exposition was
internal. One was prose in
[`api.go`](../../server/internal/api/api.go): *"internal only — not exposed
through the ingress"*. The other was an executable check in
[`smoke-test.sh`](../../deploy/scripts/smoke-test.sh) that had been wrapped in a
skip since v0.13.2 and had therefore never run in CI.

Neither was true. Everything the server serves — the REST API, the WebSocket
routes, `/healthz` and the single-page application — sits on one chi router
behind one ingress rule with `path: /` and `pathType: Prefix`. There is no
NetworkPolicy and no proxy in front. `/metrics` was reachable from the public
internet, unauthenticated, outside the rate-limited subrouter, rendering the
whole registry on every request.

The comment had been true once. Its ancestor read *"not proxied by Caddy —
internal only"*, and that edge routed only `handle /api/*` and `handle /ws/*` to
the server. SPA serving then moved into the Go binary (`-web-dir /srv/web`),
which collapsed three edge routes into one catch-all — and the comment was
updated by substituting the new edge's name for the old one without re-verifying
the claim. The skip in the smoke test dates to the same era: the old edge
answered `/metrics` with the SPA page, so the body check failed and the check
was skipped rather than inverted.

The ingress-level carve-out that would otherwise fix this is closed by a
deliberate existing decision: the chart runs the controller with
`allowSnippetAnnotations=false`, which is why security headers live in the
add-headers ConfigMap.

## Decision

**The server binds two listeners.** The public one serves the API, the WebSocket
routes, `/healthz` and the SPA. The cluster-only one
([`internal_listener.go`](../../server/internal/app/internal_listener.go)) serves
the exposition and `net/http/pprof`, on an address the binary takes as
`-internal-listen`. The Service publishes its port under the name `metrics`; the
Ingress backs onto the API port alone.

**A second port rather than a path carve-out**, because there is nowhere to
carve: any path mounted on the public router is published by the catch-all rule,
and the edge cannot deny a path either. A port is the boundary that holds in
both directions — the Service publishes what the cluster needs and the Ingress
publishes neither the port nor the paths.

**`/healthz` stays public.** The kubelet probes the container's published port.

**The profiler is registered by hand, not imported for its side effect.**
`net/http/pprof`'s `init` installs itself on `http.DefaultServeMux`, which is not
the mux this process serves and is reachable from anywhere in the binary. That
import shape is refused by
[`internal-listener.test.sh`](../../scripts/tests/internal-listener.test.sh).

**Every consumer moves in the same commit, and each one gets a read-back.**
Five things read the exposition — the in-cluster scrape, both deploy smoke
tests, `make e2e` against the compose stack, the nightly load harness, and the
acceptance tier — and three of them fail *silently* if missed: a scrape that
selects a port name nothing publishes finds no endpoint, and a probe pointed at
the API port reads the SPA fallback and passes. That is
[`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md)'s subject, so
the port is read back out of every place that names it and required to be the
same number: the chart's `server.metricsPort` against the binary's own default,
the Deployment argument, both container ports, the Service port, the scrape
job's `keep` regex and both compose stacks
([`internal-listener.test.sh`](../../scripts/tests/internal-listener.test.sh));
the forwarded port and the smoke test's `--metrics-port`
([`cd-workflow.test.sh`](../../scripts/tests/cd-workflow.test.sh)); and the
harness's metrics URL
([`loadtest-workflow.test.sh`](../../scripts/tests/loadtest-workflow.test.sh)).

**The smoke test's `--domain` branch asserts the boundary instead of skipping
it**, and CD runs a `--domain` invocation on every staging and production deploy.
A run through the public edge now requires that `/metrics` and `/debug/pprof/`
answer with something that is not the process's own internals. Asserting the
body rather than the status is deliberate: the catch-all rule sends every
unrouted path to the SPA, so a status code alone cannot tell a served page from
a served registry.

**An edge run proves it reached the edge before it asserts what the edge did
not serve.** Both boundary checks are shaped as absences, and so is a request
that never happened: an empty body matches no pattern, and `curl` reports `000`
for a transfer it could not make. So each one reads the status first. The target
is named rather than assumed for the same reason — an Ingress routes on a Host
header, which need not be a name a public resolver answers for, so the run is
handed the address the controller published for that Ingress and the scheme it
serves there. [`smoke-test-edge.test.sh`](../../scripts/tests/smoke-test-edge.test.sh)
drives the script against a stub edge that keeps the boundary, one that serves
the exposition, and one that is not listening, and requires a different verdict
from each.

**A missing `--metrics-port` fails the smoke test rather than defaulting to the
API port.** A default there would silently probe the wrong listener and report a
pass, which is the failure this whole decision is about.

## Consequences

The exposition and the profiler are reachable from inside the cluster and from
nowhere else, and that claim is now executed on every deploy rather than
asserted in a comment.

`pprof` becomes available for the first time. A pod that is misbehaving can be
profiled through a `kubectl port-forward` without a rebuild, which is directly
what the leak in
[ADR-093](ADR-093-a-relay-session-lifetime-is-owned-by-the-relay.md) would have
been diagnosed with.

The internal listener is unauthenticated by design — the boundary is the network
rather than a credential — so it carries a read-header timeout and no write
timeout, the latter because a CPU profile streams for the number of seconds it
was asked for.

The container image, both compose stacks and the chart all publish one more
port. The acceptance harness stands a second `httptest` server, which is what
keeps its instrumentation outcome stated through the door a scraper actually
uses.
