---
number: 34
title: Server keys come from a Kubernetes Secret
---

# ADR-034 — Server keys come from a Kubernetes Secret

## Context

The server's identity is four files: the certificate authority certificate and
key, the push-notification keypair, and the update-signing key. If they are
generated into a volume on first run, redeploying onto a fresh volume issues a
new authority and every enrolled agent stops being able to connect.

## Decision

**The four files are mounted read-only from the chart's existing Kubernetes
Secret**, each through its own path, and `/data` becomes writable scratch space
rather than the home of anything that matters.

The server already loads key files when it finds them, so this needs no branch
in application code.

A shared read-write filesystem was rejected: this material is written once and
read many times, which is what a Secret is for.

## Consequences

Identity survives any redeploy, and rolling updates are allowed when the network
exposure also permits overlapping pods.
