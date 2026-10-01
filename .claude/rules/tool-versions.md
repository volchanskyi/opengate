# Tool Versions — One Version, Written Down Once

**Enforced by:**
[`scripts/tests/tool-version-parity.test.sh`](../../scripts/tests/tool-version-parity.test.sh)
(gauntlet shell-tests step) over the manifest
[`scripts/lib/tool-versions.sh`](../../scripts/lib/tool-versions.sh), and
[`scripts/lib/toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh)
(gauntlet prerequisite phase), and
[`pretooluse-tool-install-guard.sh`](../hooks/pretooluse-tool-install-guard.sh)
(a typed install). **No bypass.**

Companion to [`tooling.md`](tooling.md), which governs language toolchains. This
governs everything else.

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

## What deliberately floats

- Rust `stable`, Rust `nightly`, and Node's major are asked of CI as channels.
  They are held level by
  [`toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh) refusing a
  machine that has fallen behind what CI would resolve today.
- Go is pinned exactly by `server/go.mod`, and every workflow is held to that.
