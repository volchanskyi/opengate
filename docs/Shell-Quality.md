# Shell Quality

OpenGate uses Bash for repository automation, hooks, deployment helpers,
installers, and observability transport. Quality is enforced by one pinned,
offline validation path rather than by reducing the Shell language share.

## Commands

| Command | Purpose |
|---|---|
| `make shell-check` | Parse every tracked Bash file, run ShellCheck, reject shfmt drift, and enforce execution-class policy |
| `make shell-fmt` | Format tracked Bash files |
| `make shell-test` | Run deterministic plain-Bash behavioral tests |
| `make shell-quality` | Run `shell-check` followed by `shell-test` |
| `scripts/shell-quality.sh changed <base>` | Validate changed and untracked Bash files for the agent edit loop |

The canonical runner is [`scripts/shell-quality.sh`](../scripts/shell-quality.sh).
It enumerates files through Git, so generated, ignored, and temporary files do
not alter the full-repository result.

## Tooling Policy

[`scripts/install-shell-tools.sh`](../scripts/install-shell-tools.sh) owns the
tool versions, release assets, and checksums. Provisioning is separate from
validation: checks perform no network access and fail when the exact tools are
unavailable.

- ShellCheck behavior is configured in [`.shellcheckrc`](../.shellcheckrc).
- shfmt behavior is configured by the Bash section of
  [`.editorconfig`](../.editorconfig).
- Workflow `run:` blocks are checked by actionlint using the provisioned
  ShellCheck binary in [`ci.yml`](../.github/workflows/ci.yml).
- Composite-action logic lives in adjacent tracked scripts under
  [`.github/actions/`](../.github/actions/) rather than multiline YAML blocks.

## Execution Classes

Every tracked Bash file has one enforced execution class:

1. **Standalone executable** — declares `set -euo pipefail`; `-E` is added only
   when an inherited `ERR` trap is part of the contract.
2. **Failure aggregator** — declares `set -uo pipefail` and records its reason
   in [`.claude/shell-policy.exceptions`](../.claude/shell-policy.exceptions).
3. **Sourced library** — does not mutate caller options or traps at file scope.

[`scripts/check-shell-policy.sh`](../scripts/check-shell-policy.sh) rejects bad
shebangs, missing strict mode, stale manifest rows, library option leakage, and
unapproved option disabling.

Enforcement runs in the
[`precommit gauntlet`](../scripts/precommit-gauntlet.sh), the
[`Config Lint` CI job](../.github/workflows/ci.yml), and the
[post-write agent hook](../.claude/hooks/posttooluse-shell-quality.sh).

## Behavioral Tests

Shell tests use plain Bash and offline command stubs. Stubs make real control
flow executable without OCI, Kubernetes, Docker, systemd, or network access;
assertions target exit status, files, cleanup, idempotency, payloads, and
redaction rather than merely recording that a stub ran.

Critical coverage includes:

- the [downloadable agent installer](../scripts/tests/install-sh.test.sh);
- [OCI Bastion session lifecycle](../scripts/tests/bastion-session.test.sh);
- [deploy and rollback state transitions](../scripts/tests/deploy-rollback.test.sh);
- [private in-cluster VictoriaMetrics transport](../scripts/tests/vm-transport.test.sh)
  and [legacy trend retirement](../scripts/tests/ci-trend-retirement.test.sh);
- extracted scripts under [`.github/actions/`](../.github/actions/).

## Bash or Go

Keep thin launchers, CI glue, and command orchestration in Bash. Move a tool to
Go when it owns persistent state, concurrency, a complex parser, or a
multi-command API that is difficult to test through command seams.

File size and language percentage are not migration criteria. The downloadable
agent installer remains a single Bash file so managed machines need no
additional bootstrap artifact.
