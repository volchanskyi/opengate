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

# A psql stand-in backed by one counter file per kind of residue, so a DELETE
# really changes what a later COUNT reports — a fake that always answers the same
# thing would pass a cleanup that deleted nothing. Every kind a run creates has a
# counter here, because a kind the fake cannot count is a kind the script can
# stop removing without any test noticing.
make_fake_psql() {
  local users="$1" devices="$2" delete_works="$3"
  local orgs="${4:-0}" sites="${5:-0}"
  printf '%s' "$users" >"$WORK/users"
  printf '%s' "$devices" >"$WORK/devices"
  printf '%s' "$orgs" >"$WORK/organizations"
  printf '%s' "$sites" >"$WORK/sites"
  cat >"$WORK/psql" <<EOF
#!/usr/bin/env bash
mode=stdin
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
      *"FROM organizations"*) cat "$WORK/organizations" ;;
      *"FROM sites"*) cat "$WORK/sites" ;;
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
  stdin)
    body="\$(cat)"
    printf '%s' "\$body" >"$WORK/psql-last-body.txt"
    if [ "$delete_works" = "yes" ]; then
      # Two tables point at users with no cascade. Postgres refuses to remove an
      # account while either still names it, so a removal that has not cleared
      # them first is refused here too — that refusal is the whole reason every
      # account in staging survived every cleanup.
      case "\$body" in
        *"DELETE FROM users"*)
          for dependent in enrollment_tokens agent_sessions; do
            case "\$body" in
              *"DELETE FROM \$dependent"*) ;;
              *)
                echo "ERROR:  update or delete on table \\"users\\" violates foreign key constraint on table \\"\$dependent\\"" >&2
                exit 1
                ;;
            esac
          done
          printf '0' >"$WORK/users"
          ;;
      esac
      for kind in devices sites organizations; do
        case "\$body" in
          *"DELETE FROM \$kind"*) printf '0' >"$WORK/\$kind" ;;
        esac
      done
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
make_fake_psql 81 40 yes 8 38
run_cleanup
assert_eq "a residue purge exits 0" "0" "$STATUS"
assert_eq "the accumulated users are removed" "81" "$(jq -r '.removed_users' "$WORK/proof.json")"
assert_eq "the accumulated devices are removed" "40" "$(jq -r '.removed_devices' "$WORK/proof.json")"
assert_eq "nothing is left behind" "0" "$(jq -r '.orphan_users' "$WORK/proof.json")"

# The customers and the sites under them are the kinds that survived a week of
# cleanups: nothing selected them and nothing counted them, so the next run's
# fixture collided with what the last one left and the fleet never connected.
assert_eq "the customers a run took on are removed" "8" "$(jq -r '.removed_organizations' "$WORK/proof.json")"
assert_eq "the sites under them are removed" "38" "$(jq -r '.removed_sites' "$WORK/proof.json")"
assert_eq "no customer is left behind" "0" "$(jq -r '.orphan_organizations' "$WORK/proof.json")"
assert_eq "no site is left behind" "0" "$(jq -r '.orphan_sites' "$WORK/proof.json")"

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

# Residue of any kind fails the cleanup. A run whose accounts went but whose
# customers stayed is the exact state that broke every night for a week, and it
# reported success.
make_fake_psql 0 0 no 3 0
run_cleanup
if [ "$STATUS" -ne 0 ]; then
  pass "a cleanup that left a customer behind fails"
else
  fail "a cleanup that left a customer behind fails"
fi
assert_eq "the surviving customers are counted" "3" "$(jq -r '.orphan_organizations' "$WORK/proof.json")"

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

# The one account cleanup must not touch. It is the administrator a run mints
# its enrollment token against, and a run that removes it leaves the next night
# with nobody to mint against.
if grep -q 'LOADTEST_SERVICE_ACCOUNT' "$CLEANUP"; then
  pass "the service account is exempt from the purge"
else
  fail "the service account must be exempt from the purge"
fi

make_fake_psql 3 0 yes
STATUS=0
LOADTEST_PSQL="$WORK/psql" LOADTEST_SERVICE_ACCOUNT="opengate-service@service.invalid" \
  "$CLEANUP" "$WORK/proof.json" >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
assert_eq "a purge that spares the service account still exits 0" "0" "$STATUS"
if grep -q 'opengate-service@service.invalid' "$WORK/psql-last-body.txt" 2>/dev/null; then
  pass "the purge names the account it is sparing"
else
  fail "the purge must name the spared account in its statement"
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
