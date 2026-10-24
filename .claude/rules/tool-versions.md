# Versions — Written Down Once, Patched Freely, Majors With Approval

**Enforced by:**
[`scripts/tests/tool-version-parity.test.sh`](../../scripts/tests/tool-version-parity.test.sh)
(gauntlet shell-tests step) over the manifest
[`scripts/lib/tool-versions.sh`](../../scripts/lib/tool-versions.sh), and
[`scripts/lib/toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh)
(gauntlet prerequisite phase), and
[`pretooluse-tool-install-guard.sh`](../hooks/pretooluse-tool-install-guard.sh)
(a typed install), and
[`scripts/dependency-fixes`](../../scripts/dependency-fixes/) (the gauntlet's first
check after its prerequisites, and CI's security audit) for every dependency; the
reviewer applies the rest. **No bypass.**

Companion to [`tooling.md`](tooling.md), which governs language toolchains. This
governs everything else, and which version changes any dependency takes.

## The rules

### A version is written in the manifest and nowhere else

- [`tool-versions.sh`](../../scripts/lib/tool-versions.sh) is the single home.
- The install scripts read it; the parity sweep holds every workflow's copy
  equal to it.

### An install names its version, wherever the install is written

- Refused: `cargo install <tool>`, `go install <pkg>@latest`, `pip install
  <pkg>`, an action asked for a bare `tool:` name, and `--git <url>` with no
  `--rev`.
- An unreleased tree is pinned by **rev**, never by branch.
- The sweep reads the Makefile, the install scripts and the shared actions under
  `.github/actions/` as well as the workflows.
- The workstation's installs are spelled once, in
  [`require-tool.sh`](../../scripts/require-tool.sh), which builds each command
  from the manifest.
- A typed install is held to the same line. A `go install`, `cargo install`,
  `pip`/`pipx install` of a pinned tool that names no version, `@latest` or
  another version is refused, and so is a distribution package of one (`apt`,
  `snap`, `brew`), which carries the distribution's version. A file sweep cannot
  see a command typed at the prompt.

### The runner image is named

- `runs-on:` names an image, never a moving tag.

### Both sides are checked

- The sweep holds CI to the manifest, including the tools an action installs:
  an action left to choose resolves its own version.
- Every pinned tool the gauntlet runs is version-checked on the workstation
  before the gauntlet starts, by asking the copy PATH resolves. A drifted or
  missing one is refused with the command that installs the pin, and an older
  copy earlier on PATH fails it.
- A Makefile target asks [`require-tool.sh`](../../scripts/require-tool.sh) for
  each pinned tool it runs, which refuses one present at another version, not
  just one that is absent.

### A manifest row is checked or it is not a row

- The sweep fails on a row whose tool no workflow names, and on one a workflow
  contradicts.
- It fails on a row neither the CI half nor an installer CI runs reads, and on
  a row for a tool the gauntlet runs that the workstation half does not check.
  What the gauntlet runs is read from the gauntlet: its commands, its `make`
  targets' commands, and the scripts they start.
- A tool that leaves the workflows leaves the manifest in the same commit.
- Every row names where its releases come from in a `TOOL_SOURCE_` row beside it:
  a Go module, a PyPI, crates.io or npm package, a chart repository, an image, or a
  tagged or pinned git source. The dependency check reads that source, and the
  sweep fails a row without one.

### Every dependency is checked for a patch that fixes a known hole

- Before a commit, every dependency of the project is checked for a published
  security advisory, of any severity, that a patch release fixes: npm packages, Go
  modules and the Go standard library, Rust crates, GitHub Actions, every pinned
  tool and what it pulls in, container images, charts installed into the cluster,
  Terraform providers and plugins, and the Kubernetes version.
- A patch release is one of the line already in use: the same major and minor
  version (`1.2.3` → `1.2.x`, `0.70.0` → `0.70.x`, `go1.27.3` → `go1.27.x`).
- An advisory a patch fixes is fixed in the same change, before the commit. It is
  never excepted.
- An advisory no release fixes leaves nothing to take, and this check passes it.
- A declaration the check has no reader for fails it, and so does a lookup that
  gives no answer it can read. Files that only look like a declaration are listed,
  with the reason, in [`scope.json`](../../scripts/dependency-fixes/scope.json).
- An advisory fixed only in a newer minor or major release is brought to Ivan
  explicitly the moment it is found, as a question with options to act on (in
  Claude Code, the AskUserQuestion modal). The question names the advisory, the
  dependency and the version in use, the release that fixes it, and what moving to
  that release risks. Its options include taking that release and keeping the
  version in use. Nothing moves until Ivan chooses.
- An image takes a newer patch tag of its own line. A tag that names a line
  (`17-alpine`, `24-alpine`) is read at its newest build.

### A major upgrade or an end-of-life replacement waits for Ivan's explicit approval

- Every time, for every kind of dependency listed above, and for the toolchains.
- A major upgrade changes the first non-zero version number: `11.6.0` → `12.0.0`,
  and `0.16.0` → `0.18.0`.
- An end-of-life replacement swaps a dependency its maintainers no longer support
  for another one, or for a supported line of itself.
- The question goes to Ivan before any edit, the same way: a question with options
  to act on, in plain language, saying what changes, why, and what it risks. An
  approval covers the one change it names.
- A plan, an audit, a Dependabot pull request or a failing gate is not an approval.

## What deliberately floats

- Rust `stable`, Rust `nightly`, and Node's major are asked of CI as channels.
  They are held level by
  [`toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh) refusing a
  machine that has fallen behind what CI would resolve today.
- Go is pinned exactly by `server/go.mod`, and every workflow is held to that.
