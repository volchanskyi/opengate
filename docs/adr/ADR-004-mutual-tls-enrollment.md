---
number: 4
title: ECDSA P-256 mutual TLS, with enrollment by signing request
---

# ADR-004 — ECDSA P-256 mutual TLS, with enrollment by signing request

## Context

Agents run on untrusted networks, so agent and server authenticate each other,
without depending on a public certificate authority.

## Decision

The server is its own certificate authority, on a ten-year P-256 certificate.
An agent enrolls by posting a PKCS#10 signing request to
`/api/v1/enroll/{token}`; the server ignores the request's subject, chooses the
device number, and returns a certificate for that number with the trust bundle.
How the certificate is renewed, admitted and re-signed under a new master key is
[ADR-128](ADR-128-machine-identity-lifecycle.md). TLS 1.3 minimum. A device
registers without a group — `devices.group_id` is nullable.

Enrollment tokens are issued by an administrator with a use cap and an expiry.

## Consequences

Losing the master key means re-enrolling every agent.
