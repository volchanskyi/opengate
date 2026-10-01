#!/usr/bin/env bash
# pretooluse-tool-install-guard.sh — a pinned tool installed from the command
# line is installed at its pin.
#
# Triggers on PreToolUse Bash. Refuses a `go install`, `cargo install`,
# `pip`/`pipx install`, or a distribution install (apt, apt-get, snap, brew) of a
# tool scripts/lib/tool-versions.sh pins, when it names no version, `@latest`,
# or a version other than the pin. A distribution package carries the
# distribution's version, so it is refused whatever it names.
#
# Why a hook: the parity sweep reads files, and a typed command is not a file.
# The workstation's govulncheck was replaced by a hand-typed `@latest` install;
# the gauntlet ran that copy green for days while CI ran the pin, which crashed.
#
# NO BYPASS.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"
[ -n "$cmd" ] || exit 0
grep -qE '(^|[^A-Za-z0-9_-])install([^A-Za-z0-9_-]|$)' <<<"$cmd" || exit 0

# shellcheck source=../../scripts/lib/tool-versions.sh
. "$PROJECT_ROOT/scripts/lib/tool-versions.sh"

# Each refused install, as: tool<TAB>pin<TAB>what was asked for.
refused="$(
  CMD="$cmd" python3 - <<'PYEOF'
import os, re, shlex

cmd = os.environ["CMD"]

def pin(name):
    return os.environ.get("TOOL_VERSION_" + name.upper().replace("-", "_"))

def norm(v):
    return v[1:] if v.startswith("v") else v

WRAPPERS = {"sudo", "command", "exec", "env", "nohup", "time"}
CARGO_VALUE_FLAGS = {"--version", "--vers", "--git", "--branch", "--tag", "--rev", "--path",
                     "--root", "--registry", "--index", "--features", "-F", "--target",
                     "--profile", "-j", "--jobs", "--bin", "--example", "--config", "-Z"}
PIP_VALUE_FLAGS = {"-r", "--requirement", "-c", "--constraint", "-i", "--index-url",
                   "--extra-index-url", "-t", "--target", "--python", "--pip-args", "--suffix"}

def refuse(tool, asked):
    print("%s\t%s\t%s" % (tool, pin(tool), asked))

for seg in re.split(r"\|\||&&|[;|&\n]", cmd):
    try:
        toks = shlex.split(seg)
    except ValueError:
        toks = seg.split()
    while toks and (toks[0] in WRAPPERS or re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", toks[0])):
        toks = toks[1:]
    if not toks:
        continue
    verb = os.path.basename(toks[0])
    if verb in ("python", "python3") and toks[1:3] == ["-m", "pip"]:
        verb, toks = "pip", ["pip"] + toks[3:]
    if len(toks) < 2 or toks[1] != "install":
        continue
    args = toks[2:]

    if verb == "go":
        for a in args:
            if a.startswith("-") or a.startswith("."):
                continue
            pkg, _, ver = a.partition("@")
            parts = [p for p in pkg.split("/") if not re.fullmatch(r"v[0-9]+", p)]
            tool = parts[-1] if parts else ""
            if pin(tool) and norm(ver) != norm(pin(tool)):
                refuse(tool, ver or "no version")

    elif verb == "cargo":
        version, crates, skip = "", [], False
        for i, a in enumerate(args):
            if skip:
                skip = False
                continue
            if a in CARGO_VALUE_FLAGS:
                if a in ("--version", "--vers") and i + 1 < len(args):
                    version = args[i + 1]
                skip = True
                continue
            if a.startswith("--version=") or a.startswith("--vers="):
                version = a.split("=", 1)[1]
                continue
            if a.startswith("-"):
                continue
            crates.append(a)
        for c in crates:
            name, _, ver = c.partition("@")
            ver = ver or version
            if pin(name) and norm(ver) != norm(pin(name)):
                refuse(name, ver or "no version")

    elif verb in ("pip", "pip3", "pipx"):
        skip = False
        for a in args:
            if skip:
                skip = False
                continue
            if a in PIP_VALUE_FLAGS:
                skip = True
                continue
            if a.startswith("-"):
                continue
            m = re.match(r"^([A-Za-z0-9_.-]+)(\[[^\]]*\])?(.*)$", a)
            if not m:
                continue
            name, spec = m.group(1).lower(), m.group(3)
            ver = spec[2:] if spec.startswith("==") else ""
            if pin(name) and norm(ver) != norm(pin(name)):
                refuse(name, spec or "no version")

    elif verb in ("apt", "apt-get", "snap", "brew"):
        for a in args:
            if a.startswith("-"):
                continue
            name = a.split("=", 1)[0]
            if pin(name):
                refuse(name, "the %s package" % verb)
PYEOF
)"

[ -n "$refused" ] || exit 0

msg="Bash install refused: a tool scripts/lib/tool-versions.sh pins is installed at its pin, and nothing else."
while IFS=$'\t' read -r tool want asked; do
  how="$(bash -c '. "$1"; install_command "$2"' _ "$PROJECT_ROOT/scripts/require-tool.sh" "$tool" 2>/dev/null || true)"
  msg="$msg
  $tool: asked for ${asked}, pinned at ${want}. Install it with: ${how:-the pinned version}"
done <<<"$refused"
block tool-install-off-pin "$msg
.claude/rules/tool-versions.md."
