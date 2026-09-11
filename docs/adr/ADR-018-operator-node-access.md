---
number: 18
title: Operators reach cluster nodes through OCI Bastion
---

# ADR-018 — Operators reach cluster nodes through OCI Bastion

## Context

Worker nodes have no public address, which is correct. A person occasionally
needs a shell on one anyway.

## Decision

**OCI Bastion is the human path**, defined in Terraform alongside the rest of
the network, with a helper script that caches a session so the common case is
one command.

**Automation does not use it.** CI and CD reach the cluster through the OKE
API, which authenticates as itself and leaves an audit trail that names the
workflow.

**Grafana stays internal** and is reached the same way, rather than being
exposed to get at it conveniently.

Dynamic-address workarounds were rejected: they solve the address problem
without solving the access-control one.

## Consequences

Each operator is onboarded once, and access is revoked by removing them from the
bastion rather than by rotating a key everyone shares.
