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

## What it cost

Twice now, one fact has had two homes and nothing has read both.

**jq had no home at all.** Nobody had ever chosen a version. The workstation took
whatever the distribution shipped — 1.6 — and CI took whatever the runner image
did — 1.7.1 — and the two do not render a number the same way: 1.6 canonicalises
the `17.700` a drill wrote down to `17.7`, while 1.7 preserves the literal. A
nightly drill's test asserting on that reading passed every local gauntlet and
failed every CI run, on both attempts of the same commit. Nothing in the change
pointed at jq, because no file in the repository so much as mentioned it. It is
read by 33 scripts and 8 workflows.

**Go had two homes.** `server/go.mod`'s toolchain directive was bumped to clear a
fresh stdlib advisory; the gauntlet followed it through `GOTOOLCHAIN=auto` and
went green, while CI's Security Audit job carried its own `go-version` and went
on scanning the vulnerable patch.
[`ci-govulncheck-go-version.test.sh`](../../scripts/tests/ci-govulncheck-go-version.test.sh)
is that one's guard, and it is the shape this rule generalises.

Underneath both sat `ubuntu-latest`, on all 58 jobs. A moving tag chooses jq,
python3, curl, git and coreutils for the whole of CI, and rolls to the next LTS
on GitHub's schedule rather than ours — so the tools half of CI runs on were
decided by nobody and would change on a morning nobody touched the repository.

## The rule

### A version is written in the manifest and nowhere else

[`tool-versions.sh`](../../scripts/lib/tool-versions.sh) is the single home. The
install scripts read it, so the workstation and CI provision from the same
number, and the parity sweep holds every workflow's copy equal to it. A version
re-typed into an installer is the second home the manifest exists to prevent, and
the sweep fails on it.

### An install names its version

`cargo install <tool>`, `go install <pkg>@latest`, `pip install <pkg>` and
`--git <url>` with no `--rev` all resolve at run time, which means the build is
of something nobody has compiled before. The sweep refuses each shape. Where a
tool genuinely needs an unreleased tree — the cross-compiler does — the answer is
a pinned **rev**, not a floating branch: same tree, and a decision that lands in
a diff.

### The runner image is named

`runs-on:` names an image. Everything the image ships is a dependency, and a
moving tag is a dependency nobody chose.

### Both sides are checked, because only one of them is text

The sweep reads files, so it can only hold CI to the manifest. Whether *this*
machine resolves the pinned tool is a question about a PATH, and it is answered
at run time: the gauntlet's prerequisite phase compares what `jq`, `shellcheck`
and `shfmt` actually report against the manifest and refuses a drifted
workstation with the command that fixes it. An older copy earlier on PATH is
enough to fail it, which is the case that started all this.

### A manifest row is checked or it is not a row

The sweep fails on a row whose tool no workflow names any more, as well as on one
a workflow contradicts. A row nothing checks is where the next drift begins, so a
tool that leaves the workflows leaves the manifest in the same commit.

## What deliberately floats, and why

Three toolchains ask CI for a channel rather than a release — Rust `stable`,
Rust `nightly`, and Node's major — so a pinned workstation would be the thing out
of step. They are held level the other way round, by
[`toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh) refusing a machine
that has fallen behind what CI would resolve today. Go is the exception inside
the exception: `server/go.mod` pins it exactly and every workflow is held to that.
