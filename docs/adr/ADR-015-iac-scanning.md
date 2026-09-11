---
number: 15
title: Infrastructure code is scanned by three engines
---

# ADR-015 — Infrastructure code is scanned by three engines

## Context

Terraform, the Dockerfile and the workflow files are code that grants access.
One scanner's rule database is one opinion about them.

## Decision

**Checkov is the policy scanner**, over Terraform, the Dockerfile and GitHub
Actions. Its secrets framework is deliberately off — gitleaks owns that surface,
and two tools scanning for secrets means two baselines to keep honest.

**Hadolint runs on the Dockerfile beside Checkov.** They overlap without being
redundant: Hadolint catches shell injection, instruction ordering and
bidirectional-text smuggling; Checkov catches policy — running as a user, having
a health check, pinning a base image.

**Trivy stays.** The overlap with Checkov on Terraform and the Dockerfile is
deliberate; separate rule databases catch different things on the same file.

**A baseline file is the only suppression.** Inline skip comments are not
accepted, so every exception is in one reviewable place rather than scattered
through the files it excuses.

**Infrastructure changes are gated in `ci.yml`**, and a plan that destroys is
hard-blocked on a direct push. Destroying is a deliberate act and goes through
review.

## Consequences

Three engines means three rule databases to update and occasional disagreement
about the same line, which is the cost of not trusting one vendor's opinion.
