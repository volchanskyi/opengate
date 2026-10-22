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

**The files come from Kubernetes Secrets**, and `/data` becomes writable scratch
space rather than the home of anything that matters. The push-notification
keypair and the update-signing key are mounted read-only from the chart's
existing Secret, each through its own path. The master key's role files come
from a Secret of their own, `opengate-authority`, which only the server may read
([ADR-128](./ADR-128-machine-identity-lifecycle.md)): a start-up step copies them
into a memory volume readable by the server's user alone, and `-ca-must-exist`
makes a server that finds no master key refuse to start instead of generating
one.

A shared read-write filesystem was rejected: this material is written once and
read many times, which is what a Secret is for.

## Consequences

Identity survives any redeploy, and rolling updates are allowed when the network
exposure also permits overlapping pods.
