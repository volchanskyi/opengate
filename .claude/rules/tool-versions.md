# Tool Versions — One Version, Written Down Once

**Enforced by:**
[`scripts/tests/tool-version-parity.test.sh`](../../scripts/tests/tool-version-parity.test.sh)
(gauntlet shell-tests step) over the manifest
[`scripts/lib/tool-versions.sh`](../../scripts/lib/tool-versions.sh), and
[`scripts/lib/toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh)
(gauntlet prerequisite phase), which refuses to run on a workstation whose tools
are not the pinned ones. **No bypass.**

Companion to [`tooling.md`](tooling.md), which says the local toolchains track
what CI resolves. This says what happens to everything that is *not* a language
toolchain.

A fact with two homes and nothing reading both drifts, and the drift surfaces
somewhere else. jq 1.6 and 1.7 do not render a number the same way, so a test
asserting on a reading passed every local run and failed every CI run, with
nothing in the change pointing at jq because no file mentioned it. Go's toolchain
directive was bumped to clear an advisory while CI's Security Audit job carried
its own `go-version` and went on scanning the vulnerable patch.

## The rule

### A version is written in the manifest and nowhere else

[`tool-versions.sh`](../../scripts/lib/tool-versions.sh) is the single home. The
install scripts read it, so the workstation and CI provision from the same
number, and the parity sweep holds every workflow's copy equal to it. A version
re-typed into an installer is the second home the manifest exists to prevent.

### An install names its version, wherever the install is written

`cargo install <tool>`, `go install <pkg>@latest`, `pip install <pkg>`, an action
asked for a bare `tool:` name, and `--git <url>` with no `--rev` all resolve at
run time, which means the build is of something nobody has compiled before. The
sweep refuses each shape. Where a tool genuinely needs an unreleased tree, the
answer is a pinned **rev**, not a floating branch: same tree, and a decision that
lands in a diff.

The sweep reads the Makefile and the install scripts as well as the workflows,
because an install is an install wherever it is written. `staticcheck` stopped
working outright once the Go that built it fell behind the code it analyses, and
the failure surfaced inside a gauntlet step whose subject is dead code, as an
error about export-data formats.

The workstation's installs are spelled once, in
[`require-tool.sh`](../../scripts/require-tool.sh), which builds each command
from the manifest — so a Makefile target asks for a tool by name and there is no
second number to drift. A site that reads the manifest rather than repeating its
number cannot disagree, by construction.

### The runner image is named

`runs-on:` names an image. Everything the image ships is a dependency, and a
moving tag is a dependency nobody chose — it selects jq, python3, curl, git and
coreutils for the whole of CI, and rolls to the next LTS on somebody else's
schedule.

### Both sides are checked, because only one of them is text

The sweep reads files, so it can only hold CI to the manifest. Whether *this*
machine resolves the pinned tool is a question about a PATH, answered at run
time: the gauntlet's prerequisite phase compares what `jq`, `shellcheck` and
`shfmt` report against the manifest and refuses a drifted workstation with the
command that fixes it. An older copy earlier on PATH is enough to fail it.

### A manifest row is checked or it is not a row

The sweep fails on a row whose tool no workflow names any more, as well as on one
a workflow contradicts. A row nothing checks is where the next drift begins, so a
tool that leaves the workflows leaves the manifest in the same commit.

## What deliberately floats

Three toolchains ask CI for a channel rather than a release — Rust `stable`, Rust
`nightly`, and Node's major — so a pinned workstation would be the thing out of
step. They are held level the other way round, by
[`toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh) refusing a machine
that has fallen behind what CI would resolve today.

Go is the exception inside the exception: `server/go.mod` pins it exactly and
every workflow is held to that.
