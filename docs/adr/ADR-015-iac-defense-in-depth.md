# ADR-015 — IaC Defense-in-Depth Policy Scanning

**Status**: Accepted
**Date**: 2026-05-14
**Phase**: S2 of the [IaC Security Testing Pyramid](../../.claude/plans/archive/iac-security-testing-pyramid.md) plan
**Supersedes**: —

## Context

Before this decision, infrastructure-as-code in [`deploy/`](../../deploy/) was gated by **one** policy scanner — Trivy (`trivy config`), invoked from `make lint-deploy` and the `config-lint` CI job. Trivy is excellent at high-severity misconfig pattern matching but has known gaps:

- Trivy's Dockerfile rules (~60) skew toward CVE-adjacent issues; it has no policy view on layer ordering, BIDI smuggling, version-pin hygiene.
- Trivy's GitHub Actions support is absent (it scans Kubernetes/Docker/IaC, not workflow YAML).
- A single scanner is a single point of failure: rule-engine bugs, parser bugs, and missed-pattern regressions go undetected when only one engine looks at each file.

The [IaC Security Testing Pyramid plan](../../.claude/plans/archive/iac-security-testing-pyramid.md) (PRs S1–S4) addresses both gaps by adding orthogonal scanners. This ADR records the **deliberate tool overlap** introduced by S2 and the **baseline-as-suppression** model used to manage findings the project chooses not to fix.

## Decision

1. **Add Checkov** as the primary built-in policy scanner. Framework set: `terraform`, `dockerfile`, `github_actions`. The `secrets` framework is intentionally omitted — gitleaks owns that surface (see S1 of the plan and [Security-and-Dependencies.md → Secrets scanning](../Security-and-Dependencies.md#secrets-scanning)).2. **Add Hadolint** as a Dockerfile-specific scanner alongside Checkov. The two are *orthogonal*, not redundant: Hadolint catches BIDI smuggling, RUN-merge opportunities, instruction ordering, and shell-injection patterns that Checkov's Docker rules do not; Checkov catches policy-level concerns (USER, HEALTHCHECK, base-image immutability) that Hadolint does not.
3. **Keep Trivy** in `lint-deploy` (do not retire it). The Checkov-Trivy overlap on the Dockerfile and on terraform `oci_*` resources is deliberate: each engine has its own rule database, parser, and severity model, and they regularly catch different things on the same file.
4. **Baseline-as-suppression** is the only accepted suppression mechanism for Checkov. Findings that are *not* fixable in the current PR are added to [`.checkov.baseline`](../../.checkov.baseline). Per-rule `# checkov:skip=CKV_X:reason` inline comments are NOT permitted (the project's [hooks](../../.claude/hooks/pretooluse-write-guard.sh) do not block `checkov:skip` today, but treating the baseline as the single source of authoritative exceptions keeps the surface auditable in one place).

## Currently baselined findings

`.checkov.baseline` carries no baselined findings — every Checkov check passes. Any future baseline entry records its rationale here and is reviewed quarterly.

## Consequences

**Positive:**
- Every IaC change now passes through three policy engines on the same file in CI (Trivy + Checkov + Hadolint for Dockerfile; Trivy + Checkov for terraform; Checkov for workflows).
- New findings fail the `config-lint` job immediately — the baseline only suppresses *current* findings, not future ones.
- Baseline entries carry written rationale in this ADR. Reviewing the baseline is a deliberate quarterly action, not implicit drift.

**Negative / accepted costs:**
- CI runtime for `config-lint` grows by ~45s (Checkov dominates; Hadolint is sub-second).
- Maintainers must reason about three scanners' rule IDs when triaging — partially mitigated by inline guide URLs in each finding.
- The baseline file is a small but real merge-conflict surface; rebases against `dev` may require regenerating after a `terraform fmt` change.

## Adjacent decisions (not made here)

- **Whether to retire Trivy** once Checkov and Hadolint cover the same ground. Deferred — see "Risks and tradeoffs accepted" in the plan. The user explicitly opted for defense-in-depth.
- **Whether to enable Checkov's `secrets` framework**. No — gitleaks (S1) is purpose-built; Checkov's secrets detector has higher false-positive rates against test fixtures.
- **Conftest/Rego policies** for project-specific OCI invariants land in S3, not here. See [`policy/`](../../policy/) once that PR lands.
