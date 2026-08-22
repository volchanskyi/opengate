#!/usr/bin/env bash
# Remove what a load run created, and prove it.
#
# Every user in the staging database was load-test residue: eighty-one accounts,
# every one of them matching a load-test address, in a single tenant. No run had
# ever removed what it made, and nothing ever noticed, because residue is
# invisible until somebody counts it.
#
# So a run's cleanup is not an afterthought that runs when it remembers. It runs
# on every path, it selects on the marker every load-test identity carries, and
# it writes down what it found afterwards. The count it writes is what travels
# in the evidence bundle: a run that says it left nothing has to have looked.
#
# The removal is done through the database rather than the admin API because a
# load run holds no administrator credential — and giving one a standing
# administrator would be a far larger thing than the residue it cleans.
#
# Environment:
#   LOADTEST_PSQL   the command that runs psql against the target database.
#                   Defaults to a local psql; CI passes a kubectl exec prefix.
#   LOADTEST_MARKER the marker every load-test identity carries.
#
# Usage: loadtest-cleanup.sh [cleanup-proof.json]
set -euo pipefail

MARKER="${LOADTEST_MARKER:-opengate-loadtest}"

# psql_scalar runs one query and returns the single value it produced.
psql_scalar() {
  # shellcheck disable=SC2086 # LOADTEST_PSQL is a command prefix, so it must split.
  ${LOADTEST_PSQL:-psql} -tAc "$1" | tr -d '[:space:]'
}

# psql_exec runs one statement for its effect.
psql_exec() {
  # shellcheck disable=SC2086 # LOADTEST_PSQL is a command prefix, so it must split.
  ${LOADTEST_PSQL:-psql} -q -c "$1" >/dev/null
}

# residue_users counts the accounts that belong to a load run rather than to a
# person. The historic residue predates the marker, so the address pattern the
# scenarios have always used is counted too — otherwise the eighty-one accounts
# already there would survive every cleanup that came after them.
residue_users() {
  psql_scalar "SELECT COUNT(*) FROM users WHERE email LIKE '%${MARKER}%' OR email LIKE '%@test.local'"
}

residue_devices() {
  psql_scalar "SELECT COUNT(*) FROM devices WHERE hostname LIKE '${MARKER}%' OR hostname LIKE 'soak-t%'"
}

main() {
  local out="${1:-loadtest-cleanup.json}"

  local before_users before_devices
  before_users="$(residue_users)"
  before_devices="$(residue_devices)"

  psql_exec "DELETE FROM users WHERE email LIKE '%${MARKER}%' OR email LIKE '%@test.local'"
  psql_exec "DELETE FROM devices WHERE hostname LIKE '${MARKER}%' OR hostname LIKE 'soak-t%'"

  local after_users after_devices
  after_users="$(residue_users)"
  after_devices="$(residue_devices)"

  jq -n \
    --arg marker "$MARKER" \
    --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --argjson removed_users "$((before_users - after_users))" \
    --argjson removed_devices "$((before_devices - after_devices))" \
    --argjson orphan_users "${after_users:-0}" \
    --argjson orphan_devices "${after_devices:-0}" \
    '{
      verified: true,
      marker: $marker,
      timestamp: $timestamp,
      removed_users: $removed_users,
      removed_devices: $removed_devices,
      orphan_users: $orphan_users,
      orphan_devices: $orphan_devices
    }' >"$out"

  cat "$out"

  if [ "${after_users:-0}" -ne 0 ] || [ "${after_devices:-0}" -ne 0 ]; then
    echo "::error::cleanup left residue: ${after_users} users, ${after_devices} devices" >&2
    return 1
  fi

  echo "Cleanup left nothing: removed ${before_users} users and ${before_devices} devices."
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
