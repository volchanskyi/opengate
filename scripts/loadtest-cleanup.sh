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
# An account cannot simply be deleted. Three tables name a user and none of them
# cascades — the token a run minted, the sessions a technician opened, and the
# sites somebody owns — so a plain removal is refused by the database, the script
# stops on the refusal, and nothing is cleaned at all. That is what happened
# every night for four nights: the run that could not remove its own account
# then found the address taken and failed at registration. The removal below
# clears what points at an account before removing the account, in one
# transaction, so a failure part-way leaves the database as it was.
#
# Environment:
#   LOADTEST_PSQL            the command that runs psql against the target
#                            database. Defaults to a local psql; CI passes a
#                            kubectl exec prefix.
#   LOADTEST_MARKER          the marker every load-test identity carries.
#   LOADTEST_SERVICE_ACCOUNT the one account this must never remove: the
#                            administrator a run mints its enrollment token
#                            against, seeded by the deployment rather than by a
#                            run. Removing it leaves the next night with nobody
#                            to mint against.
#
# Usage: loadtest-cleanup.sh [cleanup-proof.json]
set -euo pipefail

MARKER="${LOADTEST_MARKER:-opengate-loadtest}"
SERVICE_ACCOUNT="${LOADTEST_SERVICE_ACCOUNT:-opengate-service@service.invalid}"

# residue_predicate selects the accounts that belong to a load run rather than
# to a person, and spares the administrator a run mints against. That account is
# the chart's: a fixed id in the chart's own SQL, a password in a chart-managed
# Secret, seeded by the post-upgrade hook. A run seeds it too, so that it never
# has to assume a deploy left one behind, but seeding it is converging on
# declared state rather than creating an identity — the identities a run creates
# are the ones built from its run id, and those are what this removes. The historic
# residue predates the marker, so the address pattern the scenarios have always
# used is selected too — otherwise the accounts already there would survive
# every cleanup that came after them.
residue_predicate() {
  printf "(email LIKE '%%%s%%' OR email LIKE '%%@test.local') AND email <> '%s'" \
    "$MARKER" "$SERVICE_ACCOUNT"
}

# psql_scalar runs one query and returns the single value it produced.
psql_scalar() {
  # shellcheck disable=SC2086 # LOADTEST_PSQL is a command prefix, so it must split.
  ${LOADTEST_PSQL:-psql} -tAc "$1" | tr -d '[:space:]'
}

# psql_script runs a statement block over standard input. It is standard input
# rather than -c because psql expands neither variables nor multiple statements
# inside -c, and because one transaction is what makes a part-way failure leave
# the database exactly as it was.
psql_script() {
  # shellcheck disable=SC2086 # LOADTEST_PSQL is a command prefix, so it must split.
  ${LOADTEST_PSQL:-psql} -q -v ON_ERROR_STOP=1 >/dev/null
}

residue_users() {
  psql_scalar "SELECT COUNT(*) FROM users WHERE $(residue_predicate)"
}

residue_devices() {
  psql_scalar "SELECT COUNT(*) FROM devices WHERE hostname LIKE '${MARKER}%' OR hostname LIKE 'soak-t%'"
}

main() {
  local out="${1:-loadtest-cleanup.json}"

  local before_users before_devices
  before_users="$(residue_users)"
  before_devices="$(residue_devices)"

  # Order matters and is the whole repair. Each statement clears something that
  # names a doomed account, and the account goes last:
  #
  #   the tokens it minted → the sessions it opened → the machines it enrolled →
  #   the sites it owns → the account itself
  #
  # Removing a site unfiles its machines rather than taking them along, so the
  # machines go first and nothing is orphaned either way.
  psql_script <<SQL
BEGIN;

CREATE TEMPORARY TABLE loadtest_residue_users ON COMMIT DROP AS
  SELECT id FROM users WHERE $(residue_predicate);

DELETE FROM enrollment_tokens
 WHERE created_by IN (SELECT id FROM loadtest_residue_users);

DELETE FROM agent_sessions
 WHERE user_id IN (SELECT id FROM loadtest_residue_users);

DELETE FROM devices
 WHERE hostname LIKE '${MARKER}%'
    OR hostname LIKE 'soak-t%'
    OR site_id IN (SELECT id FROM sites
                    WHERE owner_id IN (SELECT id FROM loadtest_residue_users));

DELETE FROM sites
 WHERE owner_id IN (SELECT id FROM loadtest_residue_users);

DELETE FROM users
 WHERE id IN (SELECT id FROM loadtest_residue_users);

COMMIT;
SQL

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
