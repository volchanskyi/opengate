# W1 — Tool Provisioning, Static-Analysis Config, and Policy Tests

**Parent:** [`shell-quality-hardening.md`](shell-quality-hardening.md) · Commit 1
of 7. **Lands first on `dev`** (its test files satisfy the TDD gate for W2–W7).

## Goal

Stand up the pinned shell toolchain, the repository runner, the Make targets,
and the **failing** tests that later commits make pass — without yet enforcing
shfmt in the commit gauntlet (that is W4, after the W2 baseline).

## Why first

- Establishes the deterministic ShellCheck/shfmt versions whose absence is the
  real defect (workstation ShellCheck is `0.8.0`; pin is `0.11.0`).
- Lands at least one `*.test.sh` change on `dev`, which makes
  [`pretooluse-tdd-gate.sh`](../hooks/pretooluse-tdd-gate.sh) and
  [`pretooluse-bash-source-write-guard.sh`](../hooks/pretooluse-bash-source-write-guard.sh)
  silent for the W2 mass reformat (Guardrail A in the master plan).

## File inventory

**New:**

- `scripts/install-shell-tools.sh` — idempotent, checksum-verified provisioner
  for ShellCheck `0.11.0` and shfmt `3.13.1`. Model on
  [`scripts/install-semgrep.sh`](../../scripts/install-semgrep.sh): exact pins,
  no-op when present, fail loudly on unsupported platform, post-install version
  smoke test. **Adds a SHA-256 check** the semgrep installer doesn't need (these
  are binary assets, not a pip wheel).
- `scripts/shell-quality.sh` — the one repository runner. Subcommands:
  `check` (full repo), `changed <base>` (git-diff-scoped, for the hook latency
  budget), `format` (shfmt write), `test` (run `scripts/tests/*.test.sh`).
  Enumerates files via `git ls-files '*.sh'`, **never** filesystem traversal, so
  vendored/ignored/`target/` scripts cannot alter results.
- `.shellcheckrc` — `shell=bash`, `external-sources=true`, `enable=all` then
  re-disable only with documented repo-wide reason; severity floor `style`.
- `.editorconfig` (or extend an existing one) — shfmt-backing settings:
  `indent_style=space`, `indent_size=2`, `binary_next_line=true`,
  `switch_case_indent=true`, `shell_variant=bash`. **Do not** set the deprecated
  `keep_padding`/`-kp`.
- `scripts/tests/shell-quality.test.sh` — failing tests for: provisioner version
  pin assertion, `git ls-files` enumeration (a script under a fake `target/`
  dir is excluded), `changed <base>` selects only diffed files, and the runner
  exits non-zero on a deliberately mis-formatted fixture.
- `scripts/tests/install-shell-tools.test.sh` — provisioner is idempotent
  (second run no-ops), refuses an unsupported `uname -m`, and fails on a
  checksum mismatch (use a fixture with a wrong digest).

**Modified:**

- `Makefile` — add `shell-check`, `shell-fmt`, `shell-test`, `shell-quality`
  targets (each `command -v shellcheck/shfmt >/dev/null || { echo ERROR…; exit 1; }`
  guarded, mirroring `lint-deploy`). Add `shell-check` invocation of the
  provisioner is **not** done here from the gauntlet — see W4.
- Fix the **nine SC1091 informational** findings: add `# shellcheck source=…`
  directives next to each `source`/`.` so resolution succeeds with `-x`. Keep
  the intentional `SC2016` single-quote annotations already present in the
  gauntlet. Do not blanket-disable any rule.

## Asset references (implementer pins the SHA-256 from the release page)

- ShellCheck `0.11.0`: `shellcheck-v0.11.0.linux.x86_64.tar.xz`
  (`koalaman/shellcheck` releases). Also handle `aarch64`.
- shfmt `3.13.1`: `shfmt_v3.13.1_linux_amd64` / `_arm64`
  (`mvdan/sh` releases). Single binary, `install -m 0755`.

Install under an OpenGate tool cache (e.g. `${XDG_DATA_HOME:-$HOME/.local/share}/opengate/shell-tools`)
and symlink onto `~/.local/bin`, mirroring the semgrep precedent. Verify the
committed SHA-256 **before** extraction/install. Never `latest`, never
`curl | sh`, never an apt/package-manager version.

## Steps (TDD order)

1. Write `scripts/tests/shell-quality.test.sh` and
   `scripts/tests/install-shell-tools.test.sh` — failing (scripts don't exist).
   `chmod +x` both (the gauntlet glob requires executable).
2. Write `scripts/install-shell-tools.sh` with the two pins + checksums.
3. Write `scripts/shell-quality.sh` (`check`/`changed`/`format`/`test`).
4. Add `.shellcheckrc` and `.editorconfig` shfmt settings.
5. Resolve the nine SC1091 findings with `source=` directives.
6. Add the four Make targets.
7. Run `make shell-test` and `scripts/shell-quality.sh check` (ShellCheck only;
   **shfmt in report-only mode here** — the ~1,370-line diff is expected and is
   cleared by W2, so do not fail the runner on shfmt diff until W4 wires it into
   the gate).
8. `/precommit` → commit → `/refactor` → push.

## Out of scope (later commits)

- Wiring `shell-check` into the gauntlet or CI (**W4**).
- The shfmt reformat itself (**W2**).
- `check-shell-policy.sh` and the exception manifest (**W3**).

## Reviewer checklist

- [ ] Provisioner pins exact versions, verifies SHA-256 **before** install, is
      idempotent, fails loudly on unsupported arch, and has a post-install
      version smoke test. No `latest`, no `curl | sh`.
- [ ] `shell-quality.sh` enumerates via `git ls-files`, not `find`/globbing the
      worktree. `changed <base>` is diff-scoped.
- [ ] `.shellcheckrc` sets `shell=bash`, `external-sources=true`, no
      undocumented global rule disables.
- [ ] shfmt config matches the master plan; deprecated `keep_padding` absent.
- [ ] Both new tests are executable and **fail before** the scripts exist, pass
      after.
- [ ] The nine SC1091 findings are resolved with narrow `source=` directives,
      not blanket disables. `shellcheck --severity=style -x $(git ls-files '*.sh')`
      is clean.
- [ ] shfmt is **not** enforced in the gauntlet yet (no gate-breaking diff).
- [ ] Make targets are `command -v`-guarded like `lint-deploy`.
