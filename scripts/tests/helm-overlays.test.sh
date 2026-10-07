#!/usr/bin/env bash
# Tests eviction order and a seeded administrator that survives cleanup, which a load run needs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHART="$REPO_ROOT/deploy/helm/opengate"
PRODUCTION="$CHART/values-production.yaml"
STAGING="$CHART/values-staging.yaml"
BASE="$CHART/values.yaml"
JOB="$CHART/templates/loadtest-service-account-job.yaml"

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

echo "helm-overlays:"

value_at() {
  python3 - "$1" "$2" <<'PY'
import sys

import yaml

path, dotted = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8") as handle:
    node = yaml.safe_load(handle) or {}
for part in dotted.split("."):
    if not isinstance(node, dict) or part not in node:
        print("")
        sys.exit(0)
    node = node[part]
print(node)
PY
}

# The server reserves exactly its caps and the database its whole memory ceiling, so the
# kubelet evicts them last.
req_cpu="$(value_at "$PRODUCTION" "server.resources.requests.cpu")"
lim_cpu="$(value_at "$PRODUCTION" "server.resources.limits.cpu")"
if [ -n "$req_cpu" ] && [ "$req_cpu" = "$lim_cpu" ]; then
  pass "production server asks for exactly the processor share it is capped at"
else
  fail "production server processor requests must equal limits (req=$req_cpu lim=$lim_cpu)"
fi
for component in server postgres; do
  req_mem="$(value_at "$PRODUCTION" "$component.resources.requests.memory")"
  lim_mem="$(value_at "$PRODUCTION" "$component.resources.limits.memory")"
  if [ -n "$req_mem" ] && [ "$req_mem" = "$lim_mem" ]; then
    pass "production $component asks for exactly the memory it is capped at"
  else
    fail "production $component memory requests must equal limits (req=$req_mem lim=$lim_mem)"
  fi
done

# Each database reserves 175m under a one-processor ceiling, so the pair totals at most 350m.
rendered_db() { # values file, jq path under the postgres container's resources
  helm template opengate "$CHART" -f "$1" 2>/dev/null \
    | python3 -c '
import json, sys, yaml
for doc in yaml.safe_load_all(sys.stdin):
    if doc and doc.get("kind") == "StatefulSet":
        for c in doc["spec"]["template"]["spec"]["containers"]:
            if c["name"] == "postgres":
                print(json.dumps(c.get("resources", {})))
' | jq -r "$2 // empty | tostring"
}
command -v helm >/dev/null 2>&1 || fail "helm is not installed, so the charts cannot be rendered"
for overlay in "$PRODUCTION" "$STAGING"; do
  name="$(basename "$overlay" .yaml)"
  req="$(rendered_db "$overlay" .requests.cpu)"
  lim="$(rendered_db "$overlay" .limits.cpu)"
  if [ "$req" = "175m" ] && [ "$lim" = "1" ]; then
    pass "$name's database reserves 175m under a ceiling of one processor"
  else
    fail "$name's database must reserve 175m under a ceiling of one processor (requests=$req limits=$lim)"
  fi
done

db_pair_total="$(
  python3 - "$PRODUCTION" "$STAGING" <<'PY_PAIR'
import sys

import yaml

total = 0
for path in sys.argv[1:]:
    with open(path, encoding="utf-8") as handle:
        values = yaml.safe_load(handle) or {}
    raw = str(values.get("postgres", {}).get("resources", {}).get("requests", {}).get("cpu", "0"))
    total += int(raw[:-1]) if raw.endswith("m") else int(float(raw) * 1000)
print(total)
PY_PAIR
)"
if [ "$db_pair_total" -gt 0 ] && [ "$db_pair_total" -le 350 ]; then
  pass "the two databases reserve ${db_pair_total}m, leaving a load run the room it is sized against"
else
  fail "the two databases must reserve 1..350m together, the book a load run is sized against (got ${db_pair_total}m)"
fi

# Production's server and database leave the node room for the two generator pods of a load run.
prod_cpu_total="$(
  python3 - "$PRODUCTION" <<'PY'
import sys

import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    values = yaml.safe_load(handle) or {}
total = 0
for component in ("server", "postgres"):
    raw = str(values.get(component, {}).get("resources", {}).get("requests", {}).get("cpu", "0"))
    total += int(raw[:-1]) if raw.endswith("m") else int(float(raw) * 1000)
print(total)
PY
)"
if [ "$prod_cpu_total" -gt 0 ] && [ "$prod_cpu_total" -le 600 ]; then
  pass "production's guaranteed share leaves the node room for a run's generators"
else
  fail "production server+postgres must request 1..600m in total (got ${prod_cpu_total}m)"
fi

# Staging's server and database processor match production's; its database memory is smaller.
for field in requests.cpu requests.memory limits.cpu limits.memory; do
  staging_server="$(value_at "$STAGING" "server.resources.$field")"
  production_server="$(value_at "$PRODUCTION" "server.resources.$field")"
  if [ -n "$staging_server" ] && [ "$staging_server" = "$production_server" ]; then
    pass "staging's server $field is production's"
  else
    fail "staging's server $field must be production's (staging=$staging_server production=$production_server)"
  fi
done
for field in requests.cpu limits.cpu limits.memory; do
  staging_db="$(value_at "$STAGING" "postgres.resources.$field")"
  production_db="$(value_at "$PRODUCTION" "postgres.resources.$field")"
  if [ -n "$staging_db" ] && [ "$staging_db" = "$production_db" ]; then
    pass "staging's database $field is production's"
  else
    fail "staging's database $field must be production's (staging=$staging_db production=$production_db)"
  fi
done

if [ -f "$JOB" ]; then
  pass "the chart carries the load-test service-account job"
else
  fail "expected $JOB"
fi

if [ -f "$JOB" ] && grep -qE '"?helm\.sh/hook"?:[[:space:]]*post-install,post-upgrade' "$JOB"; then
  pass "the account is seeded by the deployment, on install and on every upgrade"
else
  fail "the job must be a post-install,post-upgrade hook"
fi

# The chart generates the password into a secret, so no literal credential sits in the repository.
SECRET="$CHART/templates/loadtest-service-account-secret.yaml"
if [ -f "$SECRET" ] && grep -q 'randAlphaNum' "$SECRET" && grep -q 'lookup "v1" "Secret"' "$SECRET"; then
  pass "the password is generated once and read back on every upgrade"
else
  fail "the password must be generated by the chart and preserved across upgrades"
fi

# The hook's statements live in their own file because the staging deploy runs them again.
SEED_SQL="$CHART/files/loadtest-account.sql"
if [ -f "$SEED_SQL" ] && grep -q "gen_salt('bf', 10)" "$SEED_SQL"; then
  pass "the stored hash is one the server's own comparison accepts"
else
  fail "the seeded account's hash must be bcrypt at the cost the server uses"
fi

# The plaintext reaches the database without crossing a command line other pod processes can read.
if [ -f "$JOB" ] && ! grep -qE -- "-v[[:space:]]+(account_)?password=" "$JOB"; then
  pass "the password never appears on a command line"
else
  fail "the password must not be passed as a psql command-line variable"
fi

enabled_staging="$(value_at "$STAGING" "loadTest.serviceAccount.enabled")"
enabled_base="$(value_at "$BASE" "loadTest.serviceAccount.enabled")"
enabled_production="$(value_at "$PRODUCTION" "loadTest.serviceAccount.enabled")"

if [ "$enabled_staging" = "True" ]; then
  pass "staging seeds the load-test service account"
else
  fail "staging must set loadTest.serviceAccount.enabled: true (got '$enabled_staging')"
fi
if [ "$enabled_base" = "False" ]; then
  pass "the chart's default leaves the account unseeded"
else
  fail "values.yaml must default loadTest.serviceAccount.enabled to false (got '$enabled_base')"
fi
if [ "$enabled_production" = "" ] || [ "$enabled_production" = "False" ]; then
  pass "production does not seed a load-test account"
else
  fail "production must not enable the load-test service account"
fi

# The cleanup selects on a marker, and the service account falls outside it so it survives.
account_email="$(value_at "$BASE" "loadTest.serviceAccount.email")"
if [ -n "$account_email" ] && [[ "$account_email" != *"opengate-loadtest"* ]] \
  && [[ "$account_email" != *"@test.local" ]]; then
  pass "the service account's address is outside every pattern cleanup selects on"
else
  fail "the service account address must not match a cleanup pattern (got '$account_email')"
fi

CLEANUP="$REPO_ROOT/scripts/loadtest-cleanup.sh"
if grep -q 'LOADTEST_SERVICE_ACCOUNT' "$CLEANUP"; then
  pass "cleanup knows which account it must never remove"
else
  fail "loadtest-cleanup.sh must exempt the service account"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
