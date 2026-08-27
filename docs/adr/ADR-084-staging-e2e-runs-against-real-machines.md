---
adr: 084
title: The Staging Browser Suite Runs Against Real Machines It Builds Itself
status: Accepted
date: 2026-08-27
---

# ADR-084: The Staging Browser Suite Runs Against Real Machines It Builds Itself

## Status

Accepted.

## Context

[ADR-081](./ADR-081-one-composition-root-and-the-acceptance-tier.md) moved the
browser specs off mocked routes and onto two real enrolled machines, named
`agent-a` and `agent-b`. The local stack runs them as containers.

The same specs run a second time, against the deployed staging release, through
a configuration that derives from the local one and deletes only the block that
brings the local stack up. That block was also what installed the machines.
Fourteen specs then asked a fleet of nothing for a machine by name, every deploy
went red on it, and staging shipped nothing for six days while production was
promoted nothing.

Three facts constrain the repair. The staging worker is a single Always-Free
arm64 node carrying production beside staging. An agent speaks QUIC over UDP,
and the port-forward the suite reaches staging through carries TCP only, so a
machine cannot run on the runner — the same conclusion the load run reached for
its own fleet. And the staging server's certificate names `localhost` alone,
because `server.quicHost` is unset there, while an agent takes its TLS name from
the host half of the address it is given and has no way to be told another.

A fourth fact decides the order of everything: an account is promoted to
administrator only when it is the sole row in `users`, and the browser suite's
own setup refuses to run if the operator it signs in as is not one.

## Decision

**Staging gets the two machines rather than the suite being narrowed.** Opening
a terminal, listing files, reading hardware inventory and restarting an agent
are the flows most likely to break on real infrastructure rather than in
compose, which is what a deploy gate is for.

**The deploy job builds their binary rather than pulling an image.** It reads
the node's architecture, cross-builds `mesh-agent` for it with the toolchain and
cache the agent release already uses, and copies the result into a stock Alpine
pod — the shape the load run uses for its own harness. The rejected alternative,
publishing an `opengate-agent` image beside the server image, needs a change-gate
of its own (`agent/**` is deliberately not an input to the server image's gate),
a tag-forward path, signing and scanning, and a way for the cluster to pull a
package that is private on creation. Worse, its gate would fire independently of
the server's, so a deploy could pair one commit's server with another commit's
machines. Building in the deploy job makes that impossible rather than guarded.

**The name a machine dials is the name on the certificate.** The chart now
defaults the machine-facing certificate's name to the server Service's own name,
which is how a pod in the namespace reaches the server in every deployment; a
deployment whose agents come from outside the cluster sets its public host
instead. The packets themselves are addressed to the server pod through a host
entry on the machine's pod, because the Service carries the HTTP port only —
that is the path the load run already carries a hundred agents over nightly.

**The bootstrap operator registers first, and nothing else takes that row.** The
database reset leaves the table empty; the deploy job then registers the operator
the suite signs in as, which is what makes it an administrator — an account is
promoted only while it is the sole row — and mints the machines' enrolment token
as that account through the public endpoint. No authority key leaves the cluster.

The administrator the nightly load run mints against goes down with that same
reset, and the run seeds it again itself, immediately before it spends it. The
deploy does not put it back: one seeder for an account only the load run reads
is one place those statements are issued from, and one place they can drift.
[ADR-085](./ADR-085-one-holder-at-a-time-over-staging.md) covers what keeps the
two runs off each other in the first place.

**One copy of the seeding statements.** They move to a file the chart's
post-upgrade hook and the deploy job both read, with the address and the password
delivered as psql variables on standard input rather than on a command line any
process in the Postgres pod or the API server's audit record would carry. A
column the schema added broke one such statement from a distance already; a
second copy would have been broken with nothing to say so.

## Consequences

The deploy job pays a cross-build — measured at two and a half to six minutes on
the agent release's warm cache — on every run that deploys staging.

A machine left behind would be a device row the next run's fleet assertions
inherit, so both pods and the spent credential are removed whatever the suite's
verdict.

The machines the specs may name are pinned in one file, and
[`e2e-stack-machines.test.sh`](../../scripts/tests/e2e-stack-machines.test.sh)
holds both stacks to it, checks the dialled name against the certificate the
chart signs, and checks that the helper gives up before the test it runs inside
does — without which its message naming the fleet it actually saw is never
printed, as it never was through fourteen failing runs.
