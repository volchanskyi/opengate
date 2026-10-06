#!/usr/bin/env bash
# Holds the workflows, the Makefile and the install scripts to scripts/lib/tool-versions.sh.
# A version literal that disagrees fails, and so does an install that names no version.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOWS="$ROOT/.github/workflows"
# An install written inside a shared action runs in every job that calls it.
ACTIONS="$ROOT/.github/actions"
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

# Each row pairs a manifest key with a grep -E pattern whose every match carries the pinned
# version; a pattern that matches nothing fails.
checked_tools=0
CHECKED_KEYS=()
check_tool() { # KEY, human name, pattern with %s where the version goes
  local key="$1" name="$2" pattern="$3" want found bad
  want="$(tool_version "$key")"
  if [ -z "$want" ]; then
    fail "$name: no TOOL_VERSION_$key in the manifest"
    return
  fi
  checked_tools=$((checked_tools + 1))
  CHECKED_KEYS+=("$key")
  found="$(grep -rhE "$pattern" "$WORKFLOWS" "$ACTIONS" "$ROOT/Makefile" "$ROOT"/scripts/*.sh || true)"
  if [ -z "$found" ]; then
    fail "$name: the manifest pins $want but nothing names it — stale row or renamed step"
    return
  fi
  # A line carrying the manifest key reads the pin, so it cannot drift.
  bad="$(grep -vF "$want" <<<"$found" | grep -vF "TOOL_VERSION_$key" || true)"
  if [ -n "$bad" ]; then
    fail "$name: pinned at $want, but an install site says otherwise:$(printf ' [%s]' "$(head -1 <<<"$bad" | sed 's/^ *//')")"
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
check_tool K6 k6 '^\s*K6_VERSION:'
check_tool CHECKOV checkov 'pipx install checkov'
check_tool YAMLLINT yamllint 'pip install --user yamllint'
check_tool CARGO_AUDIT cargo-audit 'cargo install .*cargo-audit'
check_tool CARGO_DENY cargo-deny 'cargo install .*cargo-deny'
check_tool CARGO_MODULES cargo-modules 'cargo install .*cargo-modules'
check_tool GOVULNCHECK govulncheck 'go install .*govulncheck'
check_tool GO_ARCH_LINT go-arch-lint 'go install .*go-arch-lint'
check_tool OAPI_CODEGEN oapi-codegen 'go install .*oapi-codegen'
# scripts/install-viewcore.sh reads this pin; any literal pin of the module elsewhere must agree.
check_tool VIEWCORE viewcore 'TOOL_VERSION_VIEWCORE|x/debug/cmd/viewcore@'
check_tool STATICCHECK staticcheck 'go install .*staticcheck'
check_tool GOSEC gosec 'go install .*gosec'
check_tool CARGO_FUZZ cargo-fuzz 'cargo install .*cargo-fuzz|tool: cargo-fuzz'
check_tool CARGO_NEXTEST cargo-nextest 'tool: cargo-nextest'
check_tool CARGO_LLVM_COV cargo-llvm-cov 'tool: cargo-llvm-cov'
check_tool OCI_CLI oci-cli 'pip install .*oci-cli'
# Installed by actions that resolve their own version unless told one.
check_tool ACTIONLINT actionlint '^\s*PINNED_ACTIONLINT:'
check_tool TFLINT tflint '^\s*PINNED_TFLINT:'
check_tool TRIVY trivy '^\s*PINNED_TRIVY:'
check_tool SONAR_SCANNER sonar-scanner '^\s*scannerVersion:'
check_tool SONAR_SCANNER_IMAGE sonar-scanner-image 'sonar-scanner-cli:'

if [[ "$TOOL_VERSION_SONAR_SCANNER_IMAGE" == *_"${TOOL_VERSION_SONAR_SCANNER%.*}" ]]; then
  pass "the scanner image $TOOL_VERSION_SONAR_SCANNER_IMAGE bundles the pinned CLI $TOOL_VERSION_SONAR_SCANNER"
else
  fail "the scanner image $TOOL_VERSION_SONAR_SCANNER_IMAGE does not bundle the pinned CLI $TOOL_VERSION_SONAR_SCANNER"
fi

if [ "$checked_tools" -ge 27 ]; then
  pass "$checked_tools manifest rows were checked against every install site"
else
  fail "only $checked_tools manifest rows were checked — the sweep lost rows"
fi

# A row an installer that CI runs reads straight from the manifest is held by construction.
manifest_keys="$(grep -oE '^export TOOL_VERSION_[A-Z0-9_]+=' "$ROOT/scripts/lib/tool-versions.sh" \
  | sed 's/^export TOOL_VERSION_//; s/=$//' | sort -u)"
ci_text="$(cat "$WORKFLOWS"/*.yml "$ACTIONS"/*/*.yml)"
installer_keys=""
for installer_path in "$ROOT"/scripts/install-*.sh; do
  grep -qF "$(basename "$installer_path")" <<<"$ci_text" || continue
  installer_keys="$installer_keys"$'\n'"$(grep -ohE 'TOOL_VERSION_[A-Z0-9_]+' "$installer_path" | sed 's/^TOOL_VERSION_//' || true)"
done
held_keys="$(printf '%s\n' "${CHECKED_KEYS[@]}" "$installer_keys" | grep -v '^$' | sort -u)"
unheld_keys="$(comm -23 <(printf '%s\n' "$manifest_keys") <(printf '%s\n' "$held_keys"))"
if [ -z "$manifest_keys" ]; then
  fail "the CI-half sweep read no manifest rows"
elif [ -n "$unheld_keys" ]; then
  fail "a manifest row nothing in CI is held to: $(tr '\n' ' ' <<<"$unheld_keys")"
else
  pass "all $(wc -l <<<"$manifest_keys") manifest rows are held to what CI installs"
fi

# The tools the gauntlet runs are read from its commands, its make targets' commands (make -n)
# and the scripts they start; each must be checked by scripts/lib/toolchain-parity.sh.
# shellcheck source=../lib/toolchain-parity.sh
. "$ROOT/scripts/lib/toolchain-parity.sh"
# shellcheck source=../require-tool.sh
. "$ROOT/scripts/require-tool.sh"

strip_comments() { grep -vE '^[[:space:]]*#' "$@" || true; }
gauntlet_text="$(strip_comments "$ROOT/scripts/precommit-gauntlet.sh")"
make_text=""
while IFS= read -r target; do
  make_text="$make_text"$'\n'"$(make -s -C "$ROOT" -n "$target" 2>/dev/null || true)"
done < <(grep -oE '(^|[^A-Za-z0-9_-])make [a-z][a-z0-9-]+' <<<"$gauntlet_text" | awk '{ print $NF }' | sort -u)
# require-tool.sh names every pinned tool and runs none; the make text carries its arguments.
script_text=""
while IFS= read -r script; do
  [ -f "$ROOT/$script" ] && [ "$script" != scripts/require-tool.sh ] || continue
  script_text="$script_text"$'\n'"$(strip_comments "$ROOT/$script")"
done < <(grep -oE 'scripts/[a-z0-9-]+\.sh' <<<"$gauntlet_text$make_text" | sort -u)
runs_text="$gauntlet_text$make_text$script_text"

command_re() {
  local name
  name="$(tr 'A-Z_' 'a-z-' <<<"$1")"
  case "$name" in
    # The coverage run invokes the test runner as `cargo llvm-cov nextest`.
    cargo-nextest) name="(cargo[ -]|llvm-cov )nextest" ;;
    cargo-*) name="cargo[ -]${name#cargo-}" ;;
    oci-cli) name="oci" ;;
  esac
  printf '(^|[^A-Za-z0-9_./-])%s($|[^A-Za-z0-9_-])' "$name"
}

# Prints the manifest rows of gauntlet-run tools that a check over exactly TOOL... would miss.
unchecked_rows() {
  local key tool checked_keys=""
  for tool in "$@"; do checked_keys="$checked_keys $(manifest_key "$tool")"; done
  while IFS= read -r key; do
    [ -n "$key" ] || continue
    grep -qE "$(command_re "$key")" <<<"$runs_text" || continue
    case " $checked_keys " in *" $key "*) continue ;; esac
    printf '%s\n' "$key"
  done <<<"$manifest_keys"
}

if [ -z "$make_text" ] || [ -z "$script_text" ]; then
  fail "the local-half sweep read none of what the gauntlet runs"
fi
if grep -qxF GOVULNCHECK <<<"$(unchecked_rows jq shellcheck shfmt age age-keygen zstd)"; then
  pass "the local check govulncheck drifted under misses a tool the gauntlet runs"
else
  fail "the six-tool direction no longer reproduces: govulncheck is not read as run by the gauntlet"
fi
missing_rows="$(unchecked_rows "${TOOLCHAIN_PINNED_TOOLS[@]}")"
if [ -n "$missing_rows" ]; then
  fail "the gauntlet runs a pinned tool the workstation never checks: $(tr '\n' ' ' <<<"$missing_rows")"
else
  pass "every pinned tool the gauntlet runs is checked on the workstation (${#TOOLCHAIN_PINNED_TOOLS[@]} tools)"
fi

unpinned=""
add_unpinned() { unpinned="$unpinned  $1"$'\n'; }

# Continuations are joined and comments dropped, so a --rev on the next line still pins.
mapfile -t action_sources < <(find "$ACTIONS" -type f \( -name '*.yml' -o -name '*.sh' \) | sort)
if [ "${#action_sources[@]}" -eq 0 ]; then
  fail "the install sweep found no shared action to read"
fi
install_sources=("$WORKFLOWS"/*.yml "${action_sources[@]}" "$ROOT/Makefile" "$ROOT"/scripts/install-*.sh)
install_lines="$(
  awk '
    { line = line $0 }
    /\\[[:space:]]*$/ { sub(/\\[[:space:]]*$/, "", line); next }
    { sub(/#.*$/, "", line); if (line ~ /[^[:space:]]/) print line; line = "" }
    END { if (line ~ /[^[:space:]]/) { sub(/#.*$/, "", line); print line } }
  ' "${install_sources[@]}"
)"
if [ -z "$install_lines" ]; then
  fail "the install sweep read nothing at all, so it checked no install"
fi

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

# An action given a bare tool name resolves the newest release; install-action takes name@version.
while IFS= read -r line; do
  [ -n "$line" ] || continue
  add_unpinned "action install with no version: ${line#"${line%%[![:space:]]*}"}"
done < <(grep -E '^[[:space:]]*tool:[[:space:]]*[A-Za-z]' <<<"$install_lines" \
  | grep -vE 'tool:[[:space:]]*[^[:space:]]+@' || true)

if [ -n "$unpinned" ]; then
  fail "something installs a tool without naming its version"
  printf '%s' "$unpinned" >&2
else
  pass "every workflow install names an exact version"
fi

jq_jobs_missing=""
jq_jobs_seen=0
while IFS= read -r wf; do
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

installer="$ROOT/scripts/install-shell-tools.sh"
if grep -q 'lib/tool-versions.sh' "$installer" \
  && ! grep -qE '^[A-Z_]+_VERSION="[0-9]' "$installer"; then
  pass "the shell-tool installer takes its versions from the manifest"
else
  fail "scripts/install-shell-tools.sh spells a version out instead of reading the manifest"
fi

for installer_name in install-dump-tools.sh install-viewcore.sh install-release-tools.sh; do
  installer_path="$ROOT/scripts/$installer_name"
  if grep -q 'lib/tool-versions.sh' "$installer_path" \
    && ! grep -qE '^[A-Z_]+_(VERSION|REV)="[0-9v]' "$installer_path"; then
    pass "scripts/$installer_name takes its versions from the manifest"
  else
    fail "scripts/$installer_name spells a version out instead of reading the manifest"
  fi
done

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
