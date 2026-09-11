---
number: 116
title: A presented address is believed from a named proxy
status: Accepted
date: 2026-09-10
---

# ADR-116 — A presented address is believed from a named proxy

## Context

The server counts requests per address, at about a hundred a second. A load run
drives it from one pod, so every simulated technician in a scenario and every
arriving machine in a fleet shared a single address and therefore a single
allowance between all of them.

That ceiling, not the server, was what set the nightly's throughput. Measured on
the 2026-09-05 run's own export, the three browser-side scenarios offered 59, 68
and 19 requests a second against marks of 100, 500 and 400 milliseconds, cleared
by factors of fourteen, seventy-eight and eleven. The sleep in each journey was
sized by hand to stay under the limit, which is to say the load was chosen to
avoid the thing that would have bounded it. A throughput regression could not
show, and every latency figure described a server that was idle.

The machine half sat on the same ceiling one level down. Each arrival costs an
enrolment and, since the estate is filed as it arrives
([ADR-111](ADR-111-an-estate-is-filed-as-it-arrives.md)), two more requests —
all on one address. Two profiles are above the ceiling on arrivals alone: the
spike family, whose whole subject is fifteen hundred machines coming back at
once when a site regains its link, and the largest volume leg.

Proving the mechanism took one measurement, made live against staging on
2026-09-05 from a separate pod over the same in-cluster name the generator uses:
400 requests claiming one address were answered 252 times and refused 148, and
40 requests claiming a second address, immediately afterwards, were all
answered.

That worked because of a rule that should not have been that wide. The server
believed `X-Forwarded-For` from any peer that was loopback, private or
link-local — which inside a cluster is every workload there is. A request bucket
is an identity, so a rule that wide hands the choice of identity to anything
sharing the cluster rather than to the one address requests actually enter
through.

## Decision

**The deployment names its proxies, and only a peer it named is believed.**

`server.trustedProxies` is a list. An entry is one of two things:

- a Kubernetes service, written `<service>.<namespace>`. The peer is believed
  when the cluster's own resolver says that address answers for that service.
- a range or a single address, written as a CIDR or an address.

A service is named rather than an address because the address belongs to the
controller's pod and changes whenever that pod is replaced — it is knowable to
nobody at deploy time. Reverse lookup is what turns a peer address back into a
service, and it is a real mechanism rather than an inference: asked from inside
the cluster on 2026-09-10, `10.244.0.10` answered
`10-244-0-10.ingress-nginx-controller.ingress-nginx.svc.cluster.local`, and a
pod in a set answers with its own hostname in front of the same service and
namespace. The match is on the `svc` label with the service and its namespace in
front of it and at least one label of zone behind, so the cluster's own zone
needs no second home.

Nothing named trusts nobody. Production and staging both name the edge; staging
also names the service the load run's generator pods join, which exists in that
environment alone. The disposable performance stack names the private ranges,
which is wide and is the whole of what can reach a stack created by one job,
published on one runner's loopback, and destroyed with the job.

**A load run presents one address per technician and one per machine**, from
198.18.0.0/15 — the range RFC 2544 reserves for benchmark traffic, so a
synthetic address can never be somebody real. Each browser-side scenario draws
from a block derived from its own name, because two scenarios presenting the
same address would share one technician's allowance between them.

The limit itself is untouched and is still enforced at full strength. What
changed is who the allowance belongs to.

## Consequences

The run exercises the real limiter rather than switching it off, and no
production code exists for a test's benefit — the narrowing is a security
improvement the load run then depends on, which is why the two land together
rather than one tightening the rule later and breaking the nightly the same day.

The dependency is a chain across five files: the chart's service selects a
label, the workflow's pods carry it, the chart's trusted list names that
service, the scenarios present from the shared helper, and the harness presents
per machine. A broken link does not fail loudly — every technician falls back to
one allowance, the run fills with 429s, and the night reports a regression on a
night nothing regressed. So
[`loadtest-rate-budget.test.sh`](../../scripts/tests/loadtest-rate-budget.test.sh)
reads all five and holds them level, and sizes one technician's own share
against the router's limit.

A resolver answer is remembered, a grant for five minutes and a refusal for
five seconds. The asymmetry is the generator's case: a pod that has just started
is not yet listed against its service, and a refusal kept for as long as a grant
would hold it behind one allowance for the rest of the run. A resolver that
cannot answer produces a refusal, never a grant.

What it does not solve is the product question underneath. A customer whose
technicians share one office connection still shares one allowance, and a
simultaneous rush can refuse the last of them with nothing on screen saying why.
Counting authenticated requests per account is the better design and is recorded
as its own work in [`techdebt.md`](../../.claude/techdebt.md); it would not have
solved this, because every simulated technician in a scenario shares one
sign-in, so the bucket would have moved from one address to one account.
