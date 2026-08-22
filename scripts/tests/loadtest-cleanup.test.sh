#!/usr/bin/env bash
# Tests for scripts/loadtest-cleanup.sh — a run leaves nothing behind, and says so.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CLEANUP="$REPO_ROOT/scripts/loadtest-cleanup.sh"
[ -x "$CLEANUP" ] || {
  echo "FAIL: $CLEANUP not executable" >&2
  exit 1
}

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
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# A psql stand-in backed by two counter files, so a DELETE really changes what a
# later COUNT reports — a fake that always answers the same thing would pass a
# cleanup that deleted nothing.
make_fake_psql() {
  local users="$1" devices="$2" delete_works="$3"
  printf '%s' "$users" >"$WORK/users"
  printf '%s' "$devices" >"$WORK/devices"
  cat >"$WORK/psql" <<EOF
#!/usr/bin/env bash
mode=""
query=""
while [ "\$#" -gt 0 ]; do
  case "\$1" in
    -tAc) mode=scalar; query="\$2"; shift 2 ;;
    -c) mode=exec; query="\$2"; shift 2 ;;
    *) shift ;;
  esac
done
case "\$mode" in
  scalar)
    case "\$query" in
      *"FROM users"*) cat "$WORK/users" ;;
      *"FROM devices"*) cat "$WORK/devices" ;;
    esac
    ;;
  exec)
    if [ "$delete_works" = "yes" ]; then
      case "\$query" in
        *"DELETE FROM users"*) printf '0' >"$WORK/users" ;;
        *"DELETE FROM devices"*) printf '0' >"$WORK/devices" ;;
      esac
    fi
    ;;
esac
EOF
  chmod +x "$WORK/psql"
}

run_cleanup() {
  STATUS=0
  LOADTEST_PSQL="$WORK/psql" \
    "$CLEANUP" "$WORK/proof.json" >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
}

echo "loadtest-cleanup:"

# The state that was actually found in staging: every account in the database
# was load-test residue, and no run had ever removed what it made.
make_fake_psql 81 40 yes
run_cleanup
assert_eq "a residue purge exits 0" "0" "$STATUS"
assert_eq "the accumulated users are removed" "81" "$(jq -r '.removed_users' "$WORK/proof.json")"
assert_eq "the accumulated devices are removed" "40" "$(jq -r '.removed_devices' "$WORK/proof.json")"
assert_eq "nothing is left behind" "0" "$(jq -r '.orphan_users' "$WORK/proof.json")"

# The proof travels with the run. A run that says it left nothing must have
# looked, and the looking is what the bundle carries.
assert_eq "the proof records that cleanup ran" "true" "$(jq -r '.verified' "$WORK/proof.json")"
assert_eq "the proof names the marker it selected on" "opengate-loadtest" "$(jq -r '.marker' "$WORK/proof.json")"

# A cleanup that deleted nothing must fail loudly. Reporting success on residue
# is exactly how eighty-one accounts accumulated without anyone noticing.
make_fake_psql 5 0 no
run_cleanup
if [ "$STATUS" -ne 0 ]; then
  pass "a cleanup that removed nothing fails"
else
  fail "a cleanup that removed nothing fails"
fi
assert_eq "the surviving residue is counted" "5" "$(jq -r '.orphan_users' "$WORK/proof.json")"
if grep -q "left residue" "$WORK/err.txt"; then
  pass "the failure names the residue it found"
else
  fail "the failure names the residue it found"
fi

# A clean environment is the ordinary case after the first purge, and it must
# not read as a failure.
make_fake_psql 0 0 yes
run_cleanup
assert_eq "an already-clean environment exits 0" "0" "$STATUS"
assert_eq "an already-clean environment removed nothing" "0" "$(jq -r '.removed_users' "$WORK/proof.json")"

# The historic residue predates the marker, so the address the scenarios always
# used must be selected on too — otherwise the accounts already there survive
# every cleanup that comes after them.
if grep -q '@test.local' "$CLEANUP"; then
  pass "the historic address pattern is cleaned as well as the marker"
else
  fail "the historic address pattern is cleaned as well as the marker"
fi

# Cleanup runs on every path, or a failed run is the one that leaves residue.
WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"
if awk '/loadtest-cleanup\.sh/ { found = 1 } END { exit !found }' "$WORKFLOW"; then
  pass "the workflow runs the cleanup"
else
  fail "the workflow runs the cleanup"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
