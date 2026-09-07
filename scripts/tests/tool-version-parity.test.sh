#!/usr/bin/env bash
# Every tool version is written down once, and everything else agrees with it.
#
# scripts/lib/tool-versions.sh is the manifest. This holds the workflows to it:
# a version literal that disagrees fails, and so does an install that names no
# version at all.
#
# Why a sweep and not a convention: the two skews this repository has paid for
# were both a fact with two homes and nothing reading both.
#
#   * jq had no home at all. The workstation took the distribution's 1.6 and CI
#     took the runner image's 1.7.1, and the two render a number differently —
#     1.6 canonicalises 17.700 to 17.7, 1.7 keeps the literal. A drill's test
#     asserting on that reading passed every local gauntlet and failed every CI
#     run, and no file in the repository so much as mentioned jq.
#
#   * Go had two. server/go.mod's toolchain directive was bumped to clear a
#     stdlib advisory and the gauntlet followed it; CI's Security Audit job
#     carried its own go-version and went on scanning the vulnerable patch.
#     scripts/tests/ci-govulncheck-go-version.test.sh is that case's guard, and
#     this is the same idea over every other tool.
#
# The runner image is the layer underneath both. `ubuntu-latest` is a moving tag
# that picks jq, python3, curl, git and coreutils for all of CI, and rolls to the
# next LTS on GitHub's schedule; naming the image is what turns each of those
# into a decision that lands in a diff.
#
# Run: ./scripts/tests/tool-version-parity.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOWS="$ROOT/.github/workflows"
# shellcheck source=../lib/tool-versions.sh
. "$ROOT/scripts/lib/tool-versions.sh"

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

echo "tool-version-parity:"

workflow_text="$(cat "$WORKFLOWS"/*.yml)"
[ -n "$workflow_text" ] || {
  echo "FAIL: read no workflows at all" >&2
  exit 1
}

# --- the runner image is named, never inferred -------------------------------

if grep -rqE '^\s*runs-on: *ubuntu-latest\s*$' "$WORKFLOWS"; then
  fail "a job runs on the moving ubuntu-latest tag instead of $TOOL_RUNNER_IMAGE"
else
  pass "every job names its runner image"
fi

pinned_runners="$(grep -rhcE "^\s*runs-on: *${TOOL_RUNNER_IMAGE}\s*$" "$WORKFLOWS" | awk '{s+=$1} END {print s+0}')"
if [ "$pinned_runners" -gt 0 ]; then
  pass "$pinned_runners jobs run on the pinned $TOOL_RUNNER_IMAGE"
else
  fail "no job names $TOOL_RUNNER_IMAGE — the manifest and the workflows disagree"
fi

# --- a manifest tool's version, wherever it is written, is the manifest's -----
#
# Each row is: manifest key, and a grep -E pattern whose every match must carry
# the pinned version. A pattern that matches nothing fails: a tool that has been
# renamed or removed from the workflows leaves a manifest row nothing checks,
# and a row nothing checks is how the drift starts again.
checked_tools=0
check_tool() { # KEY, human name, pattern with %s where the version goes
  local key="$1" name="$2" pattern="$3" want found bad
  want="$(tool_version "$key")"
  if [ -z "$want" ]; then
    fail "$name: no TOOL_VERSION_$key in the manifest"
    return
  fi
  checked_tools=$((checked_tools + 1))
  # Every line mentioning the tool in a version-carrying position.
  found="$(grep -rhE "$pattern" "$WORKFLOWS" || true)"
  if [ -z "$found" ]; then
    fail "$name: the manifest pins $want but no workflow names it — stale row or renamed step"
    return
  fi
  bad="$(grep -vF "$want" <<<"$found" || true)"
  if [ -n "$bad" ]; then
    fail "$name: pinned at $want, but a workflow says otherwise:$(printf ' [%s]' "$(head -1 <<<"$bad" | sed 's/^ *//')")"
    return
  fi
  pass "$name is $want everywhere it appears"
}

check_tool GITLEAKS gitleaks '^\s*GITLEAKS_VERSION:'
check_tool HADOLINT hadolint '^\s*HADOLINT_VERSION:'
check_tool HELM helm '^\s*HELM_VERSION:'
check_tool KUBECONFORM kubeconform '^\s*KUBECONFORM_VERSION:'
check_tool CONFTEST conftest '^\s*CONFTEST_VERSION:'
check_tool PMAT pmat '^\s*PMAT_VERSION:'
check_tool CARGO_MUTANTS cargo-mutants '^\s*CARGO_MUTANTS_VERSION:'
check_tool GREMLINS gremlins '^\s*GREMLINS_VERSION:'
check_tool CHECKOV checkov 'pipx install checkov'
check_tool YAMLLINT yamllint 'pip install --user yamllint'
check_tool CARGO_AUDIT cargo-audit 'cargo install .*cargo-audit'
check_tool CARGO_DENY cargo-deny 'cargo install .*cargo-deny'
check_tool CARGO_MODULES cargo-modules 'cargo install .*cargo-modules'
check_tool GOVULNCHECK govulncheck 'go install .*govulncheck'
check_tool GO_ARCH_LINT go-arch-lint 'go install .*go-arch-lint'
check_tool OAPI_CODEGEN oapi-codegen 'go install .*oapi-codegen'

if [ "$checked_tools" -ge 16 ]; then
  pass "$checked_tools manifest rows were checked against the workflows"
else
  fail "only $checked_tools manifest rows were checked — the sweep lost rows"
fi

# --- nothing installs a tool without saying which one ------------------------
#
# The shapes that resolve a version at run time. Each one has been the cause of
# a build nobody could reproduce, and `cargo install` without a version is the
# one this repository's own ci-cd-determinism rule already names.
unpinned=""
add_unpinned() { unpinned="$unpinned  $1"$'\n'; }

# Continuations are joined and comments dropped first. A `cargo install` whose
# --rev sits on the next line is pinned, and a timeout-minutes comment that
# merely mentions one is not an install at all — both read the other way to a
# sweep that takes the workflows a raw line at a time.
install_lines="$(
  awk '
    { line = line $0 }
    /\\[[:space:]]*$/ { sub(/\\[[:space:]]*$/, "", line); next }
    { sub(/#.*$/, "", line); if (line ~ /[^[:space:]]/) print line; line = "" }
    END { if (line ~ /[^[:space:]]/) { sub(/#.*$/, "", line); print line } }
  ' "$WORKFLOWS"/*.yml
)"

while IFS= read -r line; do
  [ -n "$line" ] || continue
  add_unpinned "cargo install with no version: ${line#"${line%%[![:space:]]*}"}"
done < <(grep -E '(^|[^[:alnum:]_-])cargo install ' <<<"$install_lines" \
  | grep -vE '@[0-9]|--version|--rev [0-9a-f]{40}' || true)

while IFS= read -r line; do
  [ -n "$line" ] || continue
  add_unpinned "go install with no version: ${line#"${line%%[![:space:]]*}"}"
done < <(grep -E '(^|[^[:alnum:]_-])go install ' <<<"$install_lines" \
  | grep -vE '@v?[0-9]|@\$\{\{' || true)

while IFS= read -r line; do
  [ -n "$line" ] || continue
  add_unpinned "python install with no version: ${line#"${line%%[![:space:]]*}"}"
done < <(grep -E '(pip|pipx) install ' <<<"$install_lines" | grep -vF '==' || true)

while IFS= read -r line; do
  [ -n "$line" ] || continue
  add_unpinned "git install with no rev: ${line#"${line%%[![:space:]]*}"}"
done < <(grep -E '\-\-git http' <<<"$install_lines" \
  | grep -vE '\-\-rev [0-9a-f]{40}' || true)

if [ -n "$unpinned" ]; then
  fail "a workflow installs a tool without naming its version"
  printf '%s' "$unpinned" >&2
else
  pass "every workflow install names an exact version"
fi

# --- the jobs that run jq get the pinned one ---------------------------------
#
# jq is the tool the runner image would otherwise choose, and the composite
# action is what puts the manifest's copy in front of it. A job that runs jq
# without the action is running the image's.
jq_jobs_missing=""
jq_jobs_seen=0
while IFS= read -r wf; do
  # Split the file into its jobs so the question is asked per job, not per file.
  python3 - "$wf" <<'PY' || true
import re, sys
src = open(sys.argv[1]).read()
m = re.search(r'^jobs:\s*$', src, re.M)
if not m:
    sys.exit(0)
parts = re.split(r'^  ([A-Za-z0-9_-]+):\s*$', src[m.end():], flags=re.M)
for i in range(1, len(parts), 2):
    name, blk = parts[i], parts[i + 1]
    if not re.search(r'(^|[^A-Za-z_.-])jq[ )|]', blk):
        continue
    print("SEEN")
    if 'setup-pinned-tools' not in blk:
        print("MISSING %s:%s" % (sys.argv[1].split('/')[-1], name))
PY
done < <(find "$WORKFLOWS" -name '*.yml' | sort) >"$ROOT/.jq-jobs.tmp"
jq_jobs_seen="$(grep -c '^SEEN$' "$ROOT/.jq-jobs.tmp" || true)"
jq_jobs_missing="$(grep '^MISSING ' "$ROOT/.jq-jobs.tmp" | sed 's/^MISSING //' || true)"
rm -f "$ROOT/.jq-jobs.tmp"

if [ "${jq_jobs_seen:-0}" -eq 0 ]; then
  fail "the jq sweep found no job running jq — it is checking nothing"
elif [ -n "$jq_jobs_missing" ]; then
  fail "a job runs jq without the pinned one: $(tr '\n' ' ' <<<"$jq_jobs_missing")"
else
  pass "all $jq_jobs_seen jobs running jq set up the pinned tools first"
fi

# --- the install script installs what the manifest says ----------------------
#
# The manifest is only the source of truth if the installs read it. A version
# re-typed into the installer is the second home the whole file exists to
# prevent.
installer="$ROOT/scripts/install-shell-tools.sh"
if grep -q 'lib/tool-versions.sh' "$installer" \
  && ! grep -qE '^[A-Z_]+_VERSION="[0-9]' "$installer"; then
  pass "the shell-tool installer takes its versions from the manifest"
else
  fail "scripts/install-shell-tools.sh spells a version out instead of reading the manifest"
fi

semgrep_installer="$ROOT/scripts/install-semgrep.sh"
if grep -q 'lib/tool-versions.sh' "$semgrep_installer" \
  && ! grep -qE '^SEMGREP_VERSION="[0-9]' "$semgrep_installer"; then
  pass "the semgrep installer takes its version from the manifest"
else
  fail "scripts/install-semgrep.sh spells a version out instead of reading the manifest"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
