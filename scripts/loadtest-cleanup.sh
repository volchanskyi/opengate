#!/usr/bin/env bash
# Removes what a load run created, counts what remains, and writes the count as a proof.
# It runs on every path and selects on the marker every load-test identity carries.
#
# Environment:
#   LOADTEST_PSQL  the command that runs psql against the target database (default psql)
#   LOADTEST_MARKER  the marker every load-test identity carries
#   LOADTEST_SERVICE_ACCOUNT  the seeded administrator a run mints against, never removed
#
# Usage:
#   loadtest-cleanup.sh [cleanup-proof.json]
set -euo pipefail

MARKER="${LOADTEST_MARKER:-opengate-loadtest}"
SERVICE_ACCOUNT="${LOADTEST_SERVICE_ACCOUNT:-opengate-service@service.invalid}"

# residue_predicate selects load-run accounts by the marker or the @test.local address pattern,
# and spares the seeded administrator a run mints against.
residue_predicate() {
  printf "(email LIKE '%%%s%%' OR email LIKE '%%@test.local') AND email <> '%s'" \
    "$MARKER" "$SERVICE_ACCOUNT"
}

# residue_org_predicate selects customers by the marker in their name, since a customer belongs
# to the tenant and carries no link to the account that created it.
residue_org_predicate() {
  printf "name LIKE '%s%%'" "$MARKER"
}

psql_scalar() {
  # shellcheck disable=SC2086 # LOADTEST_PSQL is a command prefix, so it must split.
  ${LOADTEST_PSQL:-psql} -tAc "$1" | tr -d '[:space:]'
}

# psql_script reads standard input since -c expands neither variables nor multiple statements,
# and one transaction leaves the database as it was after a part-way failure.
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

residue_organizations() {
  psql_scalar "SELECT COUNT(*) FROM organizations WHERE $(residue_org_predicate)"
}

residue_sites() {
  psql_scalar "SELECT COUNT(*) FROM sites WHERE name LIKE '${MARKER}%'"
}

main() {
  local out="${1:-loadtest-cleanup.json}"

  local before_users before_devices before_orgs before_sites
  before_users="$(residue_users)"
  before_devices="$(residue_devices)"
  before_orgs="$(residue_organizations)"
  before_sites="$(residue_sites)"

  # Rows go in dependency order within one transaction: tokens and sessions that name an account,
  # then machines, sites, customers and accounts; a site removal unfiles its machines.
  psql_script <<SQL
BEGIN;

CREATE TEMPORARY TABLE loadtest_residue_users ON COMMIT DROP AS
  SELECT id FROM users WHERE $(residue_predicate);

CREATE TEMPORARY TABLE loadtest_residue_orgs ON COMMIT DROP AS
  SELECT id FROM organizations WHERE $(residue_org_predicate);

DELETE FROM enrollment_tokens
 WHERE created_by IN (SELECT id FROM loadtest_residue_users)
    OR label LIKE '${MARKER}%';

DELETE FROM agent_sessions
 WHERE user_id IN (SELECT id FROM loadtest_residue_users);

DELETE FROM devices
 WHERE hostname LIKE '${MARKER}%'
    OR hostname LIKE 'soak-t%'
    OR organization_id IN (SELECT id FROM loadtest_residue_orgs);

DELETE FROM sites
 WHERE name LIKE '${MARKER}%'
    OR organization_id IN (SELECT id FROM loadtest_residue_orgs);

DELETE FROM organizations
 WHERE id IN (SELECT id FROM loadtest_residue_orgs);

DELETE FROM users
 WHERE id IN (SELECT id FROM loadtest_residue_users);

COMMIT;
SQL

  local after_users after_devices after_orgs after_sites
  after_users="$(residue_users)"
  after_devices="$(residue_devices)"
  after_orgs="$(residue_organizations)"
  after_sites="$(residue_sites)"

  jq -n \
    --arg marker "$MARKER" \
    --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --argjson removed_users "$((before_users - after_users))" \
    --argjson removed_devices "$((before_devices - after_devices))" \
    --argjson removed_organizations "$((before_orgs - after_orgs))" \
    --argjson removed_sites "$((before_sites - after_sites))" \
    --argjson orphan_users "${after_users:-0}" \
    --argjson orphan_devices "${after_devices:-0}" \
    --argjson orphan_organizations "${after_orgs:-0}" \
    --argjson orphan_sites "${after_sites:-0}" \
    '{
      verified: true,
      marker: $marker,
      timestamp: $timestamp,
      removed_users: $removed_users,
      removed_devices: $removed_devices,
      removed_organizations: $removed_organizations,
      removed_sites: $removed_sites,
      orphan_users: $orphan_users,
      orphan_devices: $orphan_devices,
      orphan_organizations: $orphan_organizations,
      orphan_sites: $orphan_sites
    }' >"$out"

  cat "$out"

  if [ "${after_users:-0}" -ne 0 ] || [ "${after_devices:-0}" -ne 0 ] \
    || [ "${after_orgs:-0}" -ne 0 ] || [ "${after_sites:-0}" -ne 0 ]; then
    echo "::error::cleanup left residue: ${after_users} users, ${after_devices} devices, ${after_orgs} customers, ${after_sites} sites" >&2
    return 1
  fi

  echo "Cleanup left nothing: removed ${before_users} users, ${before_devices} devices, ${before_orgs} customers and ${before_sites} sites."
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
