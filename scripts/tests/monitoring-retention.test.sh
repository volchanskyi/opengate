#!/usr/bin/env bash
# Offline regression test for the VictoriaMetrics retention window.
#
# The chart is the only input to the rendered retention argument: the
# statefulset passes -retentionPeriod={{ .Values.victoriametrics.retention }}
# and nothing else sets it. So a values.yaml that disagrees with what the
# cluster runs is not a deployment drift to reconcile at rollout — it is simply
# a wrong chart, and the next `helm upgrade` would change retention under the
# fleet without anyone asking for it.
#
# Both halves are asserted here, with no helm on $PATH and no conditional skip:
# values.yaml declares the window, and the template passes exactly that value
# through with no second literal anywhere in the chart. A grep that matches
# nothing fails; it never passes vacuously.
#
# Run: ./scripts/tests/monitoring-retention.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHART_DIR="$REPO_ROOT/deploy/helm/monitoring"
VALUES_FILE="$CHART_DIR/values.yaml"
STATEFULSET_FILE="$CHART_DIR/templates/victoriametrics.yaml"

# The retention the cluster runs. Changing it is a deliberate capacity decision:
# update this expectation and values.yaml together.
EXPECTED_RETENTION="30d"

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

echo "monitoring retention:"

for f in "$VALUES_FILE" "$STATEFULSET_FILE"; do
  if [ ! -f "$f" ]; then
    echo "FAIL: $f not found" >&2
    exit 1
  fi
done

# (A) values.yaml declares the retention the cluster runs.
declared="$(awk '
  /^victoriametrics:/ { in_block = 1; next }
  in_block && /^[^[:space:]]/ { exit }
  in_block && $1 == "retention:" { print $2; exit }
' "$VALUES_FILE")"

if [ -z "$declared" ]; then
  fail "values.yaml sets victoriametrics.retention (key not found — a grep that matches nothing is a failure, not a pass)"
elif [ "$declared" = "$EXPECTED_RETENTION" ]; then
  pass "values.yaml declares victoriametrics.retention: $declared"
else
  fail "values.yaml declares retention $declared, but the cluster runs $EXPECTED_RETENTION"
fi

# (B) The statefulset passes that value straight through, templated.
if grep -qF -- '-retentionPeriod={{ .Values.victoriametrics.retention }}' "$STATEFULSET_FILE"; then
  pass "statefulset renders -retentionPeriod from .Values.victoriametrics.retention"
else
  fail "statefulset must pass -retentionPeriod={{ .Values.victoriametrics.retention }} verbatim"
fi

# (C) No second retention literal anywhere in the chart can win over the value.
# A hard-coded window in an overlay or a sibling template would make (A) a lie.
hardcoded="$(grep -rn -- '-retentionPeriod=[0-9]' "$CHART_DIR" || true)"
if [ -z "$hardcoded" ]; then
  pass "no hard-coded -retentionPeriod literal in the chart"
else
  fail "chart hard-codes a retention window, bypassing the value:"
  printf '       %s\n' "$hardcoded" >&2
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
