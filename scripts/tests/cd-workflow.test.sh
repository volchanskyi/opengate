#!/usr/bin/env bash
# Tests for the staging deploy job in .github/workflows/cd.yml.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKFLOW="$SCRIPT_DIR/../../.github/workflows/cd.yml"

if [ ! -f "$WORKFLOW" ]; then
  echo "FAIL: $WORKFLOW not found" >&2
  exit 1
fi

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

assert_reset_contains() {
  local name="$1"
  local expected="$2"
  if grep -qF "$expected" <<<"$RESET_BLOCK"; then
    pass "$name"
  else
    fail "$name"
  fi
}

extract_reset_block() {
  awk '
    /^      - name: Reset staging DB for E2E$/ { in_reset = 1 }
    in_reset { print }
    /^      - name: Run Playwright E2E against staging$/ { exit }
  ' "$WORKFLOW"
}

echo "cd-workflow:"

RESET_BLOCK="$(extract_reset_block)"
if [ -n "$RESET_BLOCK" ]; then
  pass "staging DB reset step exists"
else
  fail "staging DB reset step exists"
fi

assert_reset_contains \
  "Administrators reseed includes tenant_id" \
  "INSERT INTO security_groups (id, tenant_id, name, description, is_system)"
assert_reset_contains \
  "Administrators reseed uses seeded default tenant" \
  "VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 'Administrators', 'Full system access', TRUE)"

if grep -qE '^[[:space:]]+tenants[[:space:],]*$' <<<"$RESET_BLOCK"; then
  fail "staging reset preserves the migration-seeded default tenant"
else
  pass "staging reset preserves the migration-seeded default tenant"
fi

# The app-role password reaches psql over stdin, which keeps it out of the process list.
if grep -qF -- '--set=app_password' "$WORKFLOW"; then
  fail "app-role password never rides the psql command line"
else
  pass "app-role password never rides the psql command line"
fi

if [ "$(grep -cF -- 'deploy/scripts/pg-app-role-sql.sh' "$WORKFLOW")" -eq 2 ]; then
  pass "both staging and production pipe the role SQL from the emitter"
else
  fail "both staging and production pipe the role SQL from the emitter"
fi

# Steps sit at six spaces, so a name matched at any other depth is something else.
step_line() {
  # Matched into a variable first, since pipefail reports head's early exit as a missing step.
  local found
  found="$(grep -nF -- "      - name: $1" "$WORKFLOW" || true)"
  found="${found%%$'\n'*}"
  printf '%s\n' "${found%%:*}"
}

step_body() {
  awk -v want="      - name: $1" '
    $0 == want { in_step = 1; next }
    in_step && /^      - / { exit }
    in_step { print }
  ' "$WORKFLOW"
}

assert_step_always() {
  local step="$1"
  if [ -z "$(step_line "$step")" ]; then
    fail "'$step' step exists"
    return
  fi
  pass "'$step' step exists"
  if grep -qE '^[[:space:]]+if:[[:space:]]+always\(\)[[:space:]]*$' <<<"$(step_body "$step")"; then
    pass "'$step' runs whatever the suite's verdict was"
  else
    fail "'$step' is skipped when the suite fails, so a red run leaves its residue behind"
  fi
}

ENROL_STEP="Enrol two machines against staging"
RESET_LINE="$(step_line "Reset staging DB for E2E")"
ENROL_LINE="$(step_line "$ENROL_STEP")"
E2E_LINE="$(step_line "Run Playwright E2E against staging")"

if [ -n "$ENROL_LINE" ]; then
  pass "'$ENROL_STEP' step exists"
else
  fail "'$ENROL_STEP' step exists"
fi

# The reset truncates devices, so the machines arrive after it and before the suite reads them.
# The bootstrap operator registers in the same step, as the first row of the emptied table.
if [ -n "$RESET_LINE" ] && [ -n "$ENROL_LINE" ] && [ -n "$E2E_LINE" ] \
  && [ "$RESET_LINE" -lt "$ENROL_LINE" ] && [ "$ENROL_LINE" -lt "$E2E_LINE" ]; then
  pass "the machines arrive after the reset and before the suite"
else
  fail "the machines are not brought up between the reset and the suite"
fi

if grep -qF '/api/v1/enrollment-tokens' <<<"$(step_body "$ENROL_STEP")"; then
  pass "the machines enrol with a token minted through the public endpoint"
else
  fail "the machines enrol by some path other than the public endpoint — no key may leave the cluster"
fi

assert_step_always "Remove the staging machines"
if grep -qF 'loadtest-account-sql.sh' "$WORKFLOW"; then
  fail "the deploy seeds the load-test administrator, which the run that needs it does for itself"
else
  pass "the deploy leaves the load-test administrator to the run that needs it"
fi

# The CD cache token only reads, so the image workflow builds the agent binary and CD downloads it.

for forbidden in \
  'Swatinem/rust-cache' \
  'dtolnay/rust-toolchain' \
  'cargo install cross' \
  'cross build' \
  'cargo build'; do
  if grep -qF -- "$forbidden" "$WORKFLOW"; then
    fail "cd.yml still carries '$forbidden' — a workflow whose cache token cannot write must not keep state or build from cold"
  else
    pass "cd.yml carries no '$forbidden'"
  fi
done

DOWNLOAD_STEP="Download the agent built for the staging node"
if [ -n "$(step_line "$DOWNLOAD_STEP")" ]; then
  pass "'$DOWNLOAD_STEP' step exists"
else
  fail "'$DOWNLOAD_STEP' step exists"
fi

DOWNLOAD_BODY="$(step_body "$DOWNLOAD_STEP")"
if grep -qF 'actions/download-artifact@' <<<"$DOWNLOAD_BODY"; then
  pass "the agent binary comes from an artifact"
else
  fail "the agent binary comes from an artifact"
fi

if grep -qE '^[[:space:]]+run-id:' <<<"$DOWNLOAD_BODY"; then
  pass "the download names the run it takes the artifact from"
else
  fail "the download names no run-id, so it can only see this run's own artifacts"
fi

LOCATE_STEP="Locate the run that built the agent"
LOCATE_BODY="$(step_body "$LOCATE_STEP")"
if [ -n "$LOCATE_BODY" ]; then
  pass "'$LOCATE_STEP' step exists"
else
  fail "'$LOCATE_STEP' step exists"
fi

if grep -qF '::error::' <<<"$LOCATE_BODY" \
  && grep -qF 'IMAGE_TAG' <<<"$LOCATE_BODY" \
  && grep -qF 'RUN_ID' <<<"$LOCATE_BODY"; then
  pass "a missing artifact fails the job naming the tag and the run"
else
  fail "a missing artifact does not fail loudly with the tag and the run named"
fi

# The dispatch path resolves its own run, so the machines' binary comes from one place.
if grep -qF 'workflow_dispatch' <<<"$LOCATE_BODY"; then
  pass "a dispatched tag resolves the run that built its agent"
else
  fail "a dispatched tag has no way to reach the run that built its agent"
fi

# The production deploy upgrades the monitoring release from its chart every time.
PROD_BLOCK="$(awk '
  /^  deploy-production-k8s:/ { inside = 1 }
  inside && /^  [a-z][a-z0-9-]*:$/ && !/deploy-production-k8s/ { exit }
  inside { print }
' "$WORKFLOW")"
MON_UPGRADE="$(grep -E 'helm upgrade --install monitoring deploy/helm/monitoring' <<<"$PROD_BLOCK" || true)"
if [ -n "$MON_UPGRADE" ]; then
  pass "the production deploy upgrades the monitoring release from its chart"
else
  fail "the production deploy never upgrades the monitoring release, so the cluster runs whatever was installed by hand"
fi
if grep -qF -- '-f deploy/helm/monitoring/values-production.yaml' <<<"$PROD_BLOCK" \
  && [ -f "$SCRIPT_DIR/../../deploy/helm/monitoring/values-production.yaml" ]; then
  pass "with the production overlay the release was installed with"
else
  fail "the monitoring upgrade must apply deploy/helm/monitoring/values-production.yaml"
fi

# Dashboards, rules and scrape targets land with the deploy, then the store is asked what it loaded.
apply_facts="$(
  python3 - "$WORKFLOW" <<'PY_APPLY'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
steps = doc["jobs"]["deploy-production-k8s"]["steps"]
runs = [str(s.get("run", "")) for s in steps]
upgrade = next((i for i, r in enumerate(runs) if "helm upgrade --install monitoring" in r), None)
applied = next((i for i, r in enumerate(runs) if "deploy/scripts/monitoring-config-apply.sh" in r), None)
chat = (steps[applied].get("env") or {}).get("TELEGRAM_CHAT_ID", "") if applied is not None else ""
print("after" if upgrade is not None and applied is not None and applied > upgrade else "missing")
print(chat)
PY_APPLY
)"
if [ "$(sed -n 1p <<<"$apply_facts")" = "after" ]; then
  pass "the production deploy applies the monitoring configuration after the monitoring upgrade"
else
  fail "the production deploy must run deploy/scripts/monitoring-config-apply.sh after the monitoring upgrade"
fi
if [ "$(sed -n 2p <<<"$apply_facts")" = "\${{ secrets.DEPLOY_TELEGRAM_CHAT_ID }}" ]; then
  pass "and hands it the chat the alerts are routed to"
else
  fail "and hands it the chat the alerts are routed to (got=[$(sed -n 2p <<<"$apply_facts")])"
fi

# The exposition and the profiler answer on the server's second listener, so its port is forwarded.
# The forwarded port is read back against the chart's own.

VALUES="$SCRIPT_DIR/../../deploy/helm/opengate/values.yaml"
METRICS_PORT="$(awk '/^[[:space:]]+metricsPort:/ { print $2; exit }' "$VALUES")"

# shellcheck disable=SC2016 # the pattern is the workflow's literal text, not an expansion
PF_LINES="$(grep -F 'port-forward "svc/${RELEASE}-server"' "$WORKFLOW" || true)"
if [ -n "$PF_LINES" ]; then
  pass "the deploy port-forwards the server Service"
else
  fail "no port-forward of the server Service found"
fi

# Staging and production each run their own smoke test, so every forward carries the internal port.
PF_COUNT="$(printf '%s\n' "$PF_LINES" | grep -c . || true)"
PF_WITH_METRICS="$(printf '%s\n' "$PF_LINES" | grep -cF ":${METRICS_PORT}" || true)"
if [ -n "$METRICS_PORT" ] && [ "$PF_COUNT" -gt 0 ] && [ "$PF_COUNT" -eq "$PF_WITH_METRICS" ]; then
  pass "every port-forward carries the chart's internal port ($METRICS_PORT)"
else
  fail "a port-forward omits the chart's internal port ($METRICS_PORT), so the exposition is unreachable"
fi

# The local port the forward binds and the port the smoke test reads are the same number.
LOCAL_METRICS_PORT="$(printf '%s\n' "$PF_LINES" | grep -oE "[0-9]+:${METRICS_PORT}\b" | head -n 1 | cut -d: -f1)"
SMOKE_HOST_RUNS="$(grep -F 'smoke-test.sh --host' "$WORKFLOW" || true)"
SMOKE_HOST_COUNT="$(printf '%s\n' "$SMOKE_HOST_RUNS" | grep -c . || true)"
SMOKE_WITH_PORT="$(printf '%s\n' "$SMOKE_HOST_RUNS" | grep -cF -- "--metrics-port ${LOCAL_METRICS_PORT}" || true)"
if [ -n "$LOCAL_METRICS_PORT" ] && [ "$SMOKE_HOST_COUNT" -gt 0 ] \
  && [ "$SMOKE_HOST_COUNT" -eq "$SMOKE_WITH_PORT" ]; then
  pass "every smoke run reads the exposition off the forwarded port ($LOCAL_METRICS_PORT)"
else
  fail "a smoke run does not name the forwarded internal port, so its metrics check reads the wrong listener"
fi

if grep -qF 'smoke-test.sh --domain' "$WORKFLOW"; then
  pass "a smoke run goes through the public edge, where the boundary is provable"
else
  fail "nothing runs the smoke test through the public edge, so the boundary is a claim nothing executes"
fi

# Staging's Ingress host does not resolve publicly and the chart terminates no TLS there.
# The run is handed an address and a scheme, both read back against the chart.
STAGING_VALUES="$SCRIPT_DIR/../../deploy/helm/opengate/values-staging.yaml"
STAGING_TLS="$(awk '/^ingress:/ { in_block = 1; next }
                    in_block && /^[^[:space:]]/ { exit }
                    in_block && /^[[:space:]]+tls:/ { print $2; exit }' "$STAGING_VALUES")"
if [ "$STAGING_TLS" = "true" ]; then
  EXPECTED_SCHEME=https
else
  EXPECTED_SCHEME=http
fi

# shellcheck disable=SC2016 # the pattern is the workflow's literal text, not an expansion
if grep -qF -- '--edge-address "$EDGE"' "$WORKFLOW" \
  && grep -qF 'status.loadBalancer.ingress[0].ip' "$WORKFLOW"; then
  pass "the edge run is given the address the controller published for the Ingress"
else
  fail "the edge run must be given the Ingress's own address, or it reaches the runner instead"
fi

if grep -qF -- "--scheme $EXPECTED_SCHEME --edge-address" "$WORKFLOW"; then
  pass "the edge run asks for the scheme the chart serves there ($EXPECTED_SCHEME)"
else
  fail "the edge run's scheme must match values-staging.yaml's ingress.tls ($EXPECTED_SCHEME)"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
