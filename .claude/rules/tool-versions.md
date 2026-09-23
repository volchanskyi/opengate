# Tool Versions — One Version, Written Down Once

**Enforced by:**
[`scripts/tests/tool-version-parity.test.sh`](../../scripts/tests/tool-version-parity.test.sh)
(gauntlet shell-tests step) over the manifest
[`scripts/lib/tool-versions.sh`](../../scripts/lib/tool-versions.sh), and
[`scripts/lib/toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh)
(gauntlet prerequisite phase). **No bypass.**

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
- The sweep reads the Makefile and the install scripts as well as the workflows.
- The workstation's installs are spelled once, in
  [`require-tool.sh`](../../scripts/require-tool.sh), which builds each command
  from the manifest.

### The runner image is named

- `runs-on:` names an image, never a moving tag.

### Both sides are checked

- The sweep holds CI to the manifest.
- The gauntlet's prerequisite phase compares what `jq`, `shellcheck` and `shfmt`
  report against the manifest and refuses a drifted workstation with the command
  that fixes it. An older copy earlier on PATH fails it.

### A manifest row is checked or it is not a row

- The sweep fails on a row whose tool no workflow names, and on one a workflow
  contradicts.
- A tool that leaves the workflows leaves the manifest in the same commit.

## What deliberately floats

- Rust `stable`, Rust `nightly`, and Node's major are asked of CI as channels.
  They are held level by
  [`toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh) refusing a
  machine that has fallen behind what CI would resolve today.
- Go is pinned exactly by `server/go.mod`, and every workflow is held to that.
