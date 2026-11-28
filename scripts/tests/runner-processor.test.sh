#!/usr/bin/env bash
# Tests for scripts/runner-processor.sh, which permits or refuses a runner by the processor lscpu
# names. The lscpu stand-in prints the lines lscpu prints on GitHub's runners.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/runner-processor.sh"
[ -x "$CHECK" ] || {
  echo "FAIL: $CHECK not executable" >&2
  exit 1
}
# shellcheck source=scripts/lib/refused-processors.sh
. "$REPO_ROOT/scripts/lib/refused-processors.sh"

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
assert_eq() {
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
}
assert_has() {
  if grep -qF -- "$2" <<<"$3"; then pass "$1"; else fail "$1 (no [$2] in [$3])"; fi
}

# The model strings lscpu printed on the runners the speed nightly and the legs have read.
SEEN=(
  "AMD EPYC 7763 64-Core Processor"
  "Intel(R) Xeon(R) 6975P-C"
  "AMD EPYC 9V74 80-Core Processor"
  "AMD EPYC 9V45 96-Core Processor"
  "Neoverse-N2"
)

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"
cat >"$WORK/bin/lscpu" <<EOF
#!/usr/bin/env bash
printf 'Architecture:                         x86_64\n'
if [ -s "$WORK/model" ]; then
  printf 'Model name:                           %s\n' "\$(cat "$WORK/model")"
fi
printf 'CPU(s):                               4\n'
EOF
chmod +x "$WORK/bin/lscpu"
export PATH="$WORK/bin:$PATH"

# check MODEL runs the check on a runner lscpu names MODEL, with step files of its own.
check() {
  printf '%s' "$1" >"$WORK/model"
  : >"$WORK/summary"
  rc=0
  out="$(GITHUB_STEP_SUMMARY="$WORK/summary" "$CHECK" 2>"$WORK/err")" || rc=$?
  summary="$(cat "$WORK/summary")"
}

echo "runner-processor:"

check "AMD EPYC 7763 64-Core Processor"
assert_eq "the 7763 is read" "0" "$rc"
assert_has "the 7763 is refused" "permitted=false" "$out"
assert_has "the refused processor is named" "processor=AMD EPYC 7763 64-Core Processor" "$out"
assert_has "a refusal says so on the summary page" "AMD EPYC 7763 64-Core Processor" "$summary"

for model in "Intel(R) Xeon(R) 6975P-C" "AMD EPYC 9V74 80-Core Processor" \
  "AMD EPYC 9V45 96-Core Processor" "Neoverse-N2"; do
  check "$model"
  assert_eq "$model is read" "0" "$rc"
  assert_has "$model is permitted" "permitted=true" "$out"
  assert_has "$model is named" "processor=$model" "$out"
  assert_eq "$model leaves the summary page alone" "" "$summary"
done

check ""
assert_eq "a runner lscpu names no model for fails loudly" "2" "$rc"
assert_has "the empty model is said" "no processor model" "$(cat "$WORK/err")"

mapfile -t refused_list < <(refused_processors)
assert_eq "the refused list holds one processor" "1" "${#refused_list[@]}"
for refused in "${refused_list[@]}"; do
  matched=false
  for model in "${SEEN[@]}"; do
    case "$model" in *"$refused"*) matched=true ;; esac
  done
  assert_eq "$refused matches a processor a runner has named" "true" "$matched"
done

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
