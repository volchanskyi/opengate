---
number: 9
title: Container images are signed without keys
---

# ADR-009 — Container images are signed without keys

## Context

A signing key is one more secret to store and rotate.

## Decision

Keyless signing through GitHub's OIDC identity and Sigstore. Deploys verify the
signature before doing anything, and a software bill of materials is attached to
the image. The signature ties an image to the pipeline that built it.

## Consequences

Verification adds a few seconds to a deploy, and a failed verification stops it.
