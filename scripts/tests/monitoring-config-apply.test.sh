#!/usr/bin/env bash
# The monitoring configuration the cluster is actually given, held to the
# repository that declares it.
#
# Three ConfigMaps carry everything Grafana alerts on and everything
# VictoriaMetrics scrapes. Two of them were created once by hand and re-created
# by nothing; the third is the chart's, and the chart had not been upgraded in
# over a hundred days. So the cluster evaluated seven of the thirteen rules the
# repository declares, served nine of its thirteen dashboards, and scraped none
# of the per-container series — including the ones the rule written to detect
# exactly that gap reads. A rule can be added, pinned by a gate, reviewed,
# merged, and never exist.
#
# The applier closes it, and this drives it against a stub cluster: one that
# accepts what it is given, one that quietly keeps its old content, and one that
# holds nothing at all. An apply has to pass on the first and fail on the other
# two, because the apply is not the guarantee — the read-back is.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
APPLY="$REPO_ROOT/deploy/scripts/monitoring-config-apply.sh"

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

[ -f "$APPLY" ] || {
  echo "FAIL: $APPLY missing" >&2
  exit 1
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$WORK/bin" "$WORK/state"

# --- the stub cluster ---------------------------------------------------------
#
# A real `kubectl apply` of a ConfigMap either lands or refuses, and the failure
# this file exists for is neither: it is an apply nobody ran. So the stub stores
# what it is given and answers `get` from the store, and its modes are the three
# ways the store can disagree with the repository.
cat >"$WORK/bin/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"$FAKE_KUBECTL_CALLS"

store="$FAKE_KUBECTL_STATE"

# Strip the leading `-n <ns>`, which every call in the script passes.
args=("$@")
if [ "${args[0]:-}" = "-n" ]; then
  args=("${args[@]:2}")
fi

case "${args[0]:-}" in
  get)
    name="${args[2]:-}"
    if [ "$FAKE_KUBECTL_MODE" = "empty" ] || [ ! -f "$store/$name.json" ]; then
      echo "Error from server (NotFound): configmaps \"$name\" not found" >&2
      exit 1
    fi
    cat "$store/$name.json"
    ;;
  apply | patch | create | replace)
    if [ "$FAKE_KUBECTL_MODE" = "silent-drop" ]; then
      # An apply that reports success and changes nothing. This is the whole
      # subject: the warning is not the signal, the read-back is.
      exit 0
    fi
    payload="$(cat)"
    name="$(printf '%s' "$payload" | python3 -c 'import json,sys; print(json.load(sys.stdin)["metadata"]["name"])')"
    printf '%s' "$payload" >"$store/$name.json"
    echo "configmap/$name configured"
    ;;
  rollout)
    # A restart names a workload the chart actually runs, by the kind it runs
    # as. Anything else is the NotFound a real cluster answers with.
    target="${args[2]:-}"
    if ! grep -qxF -- "$target" "$FAKE_KUBECTL_WORKLOADS"; then
      echo "Error from server (NotFound): $target not found" >&2
      exit 1
    fi
    echo "$target restarted"
    ;;
  *)
    echo "unexpected kubectl call: $*" >&2
    exit 1
    ;;
esac
FAKE_KUBECTL
chmod +x "$WORK/bin/kubectl"

# The workloads the monitoring chart runs, as `kind/name`, read from the chart
# rather than written here, so a workload that changes kind changes what the
# stub cluster holds.
python3 - "$REPO_ROOT/deploy/helm/monitoring/templates" >"$WORK/workloads" <<'CHART'
import pathlib, re, sys
for f in sorted(pathlib.Path(sys.argv[1]).glob("*.yaml")):
    text = f.read_text()
    comp = re.search(r'mon\.componentName" \(list \. "([^"]+)"\)', text)
    for kind in re.findall(r"^kind: (Deployment|StatefulSet)$", text, re.M):
        if comp:
            print(f"{kind.lower()}/monitoring-{comp.group(1)}")
CHART
[ -s "$WORK/workloads" ] || {
  echo "FAIL: no workloads read from the monitoring chart" >&2
  exit 1
}

run_apply() {
  local mode="$1" out="$2"
  shift 2
  rm -f "$WORK/calls"
  : >"$WORK/calls"
  PATH="$WORK/bin:$PATH" \
    FAKE_KUBECTL_MODE="$mode" \
    FAKE_KUBECTL_STATE="$WORK/state" \
    FAKE_KUBECTL_CALLS="$WORK/calls" \
    FAKE_KUBECTL_WORKLOADS="$WORK/workloads" \
    TELEGRAM_CHAT_ID="-1001234567890" \
    bash "$APPLY" "$@" >"$out" 2>&1
}

# stored_key NAME KEY — the content the stub cluster now holds for one key.
stored_key() {
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"].get(sys.argv[2], ""))' \
    "$WORK/state/$1.json" "$2" 2>/dev/null || printf ''
}

echo "monitoring configuration apply:"

# --- an accepting cluster: all three land and read back -----------------------
rm -f "$WORK"/state/*.json
OUT="$WORK/accept.out"
if run_apply accept "$OUT"; then
  pass "an accepting cluster reports success"
else
  fail "an accepting cluster reports success (out=[$(cat "$OUT")])"
fi

for cm in grafana-alerting grafana-dashboards monitoring-victoriametrics-scrape; do
  if [ -f "$WORK/state/$cm.json" ]; then
    pass "$cm reaches the cluster"
  else
    fail "$cm reaches the cluster"
  fi
done

# Every alert rule the repository declares is in what was sent. The gap this
# closes was seven rules of thirteen, so a count is the assertion.
REPO_RULES="$(grep -cE '^[[:space:]]+title:' "$REPO_ROOT/deploy/grafana/provisioning/alerting/alert-rules.yml" || true)"
SENT_RULES="$(grep -cE '^[[:space:]]+title:' <<<"$(stored_key grafana-alerting alert-rules.yml)" || true)"
if [ "$REPO_RULES" -gt 0 ] && [ "$SENT_RULES" -eq "$REPO_RULES" ]; then
  pass "all $REPO_RULES declared alert rules reach the cluster"
else
  fail "all declared alert rules reach the cluster (repo=$REPO_RULES sent=$SENT_RULES)"
fi

# Every dashboard the repository declares is in what was sent.
REPO_DASH="$(find "$REPO_ROOT/deploy/grafana/provisioning/dashboards" -maxdepth 1 -type f | wc -l)"
SENT_DASH="$(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["data"]))' "$WORK/state/grafana-dashboards.json" 2>/dev/null || echo 0)"
if [ "$REPO_DASH" -gt 0 ] && [ "$SENT_DASH" -eq "$REPO_DASH" ]; then
  pass "all $REPO_DASH declared dashboards reach the cluster"
else
  fail "all declared dashboards reach the cluster (repo=$REPO_DASH sent=$SENT_DASH)"
fi

# The scrape job whose absence hid Finding 3's neighbour.
if grep -qF "kubernetes-cadvisor" <<<"$(stored_key monitoring-victoriametrics-scrape scrape.yml)"; then
  pass "the per-container scrape job reaches the cluster"
else
  fail "the per-container scrape job reaches the cluster"
fi

# --- the chat id arrives as a literal string ----------------------------------
#
# Grafana refuses to start when the chat id is an unsubstituted variable, and
# starts with a broken destination when the value carries its own quotes. Only
# the literal works, and only a reading of what was sent can tell them apart.
CONTACT="$(stored_key grafana-alerting contact-points.yml)"
if grep -qF 'chatid: "-1001234567890"' <<<"$CONTACT"; then
  pass "the chat id is rendered as a quoted literal"
else
  fail "the chat id is rendered as a quoted literal (got [$(grep -F chatid <<<"$CONTACT" || echo none)])"
fi
if grep -qF 'TELEGRAM_CHAT_ID' <<<"$CONTACT"; then
  fail "no unsubstituted variable survives into the file"
else
  pass "no unsubstituted variable survives into the file"
fi
if grep -qE 'contactPoints: *\[\]' <<<"$CONTACT"; then
  fail "the contact point list is not empty"
else
  pass "the contact point list is not empty"
fi

# And the default route points at it, which is the half that decides whether a
# firing rule reaches anybody.
POLICIES="$(stored_key grafana-alerting notification-policies.yml)"
if grep -qE 'receiver: *telegram' <<<"$POLICIES"; then
  pass "the default route points at the Telegram destination"
else
  fail "the default route points at the Telegram destination (got [$POLICIES])"
fi

# --- a cluster that quietly keeps its old content: the apply fails ------------
#
# The failure the whole applier exists to refuse. Nothing warns, nothing errors,
# and the only way to know is to ask for what was written back.
rm -f "$WORK"/state/*.json
OUT="$WORK/silent.out"
if run_apply silent-drop "$OUT"; then
  fail "an apply the cluster silently dropped fails"
else
  pass "an apply the cluster silently dropped fails"
fi
if grep -qF "::error::" "$OUT"; then
  pass "and says which ConfigMap did not come back"
else
  fail "and says which ConfigMap did not come back (out=[$(cat "$OUT")])"
fi

# --- a chat id carrying its own quotes is refused before it is applied --------
#
# It is a green apply with a destination Telegram rejects at send time, which is
# the one shape a read-back of the ConfigMap cannot catch.
rm -f "$WORK"/state/*.json
OUT="$WORK/quoted.out"
if PATH="$WORK/bin:$PATH" \
  FAKE_KUBECTL_MODE=accept \
  FAKE_KUBECTL_STATE="$WORK/state" \
  FAKE_KUBECTL_CALLS="$WORK/calls" \
  FAKE_KUBECTL_WORKLOADS="$WORK/workloads" \
  TELEGRAM_CHAT_ID='"-1001234567890"' \
  bash "$APPLY" >"$OUT" 2>&1; then
  fail "a chat id carrying its own quotes is refused"
else
  pass "a chat id carrying its own quotes is refused"
fi

# --- an absent chat id is refused ---------------------------------------------
rm -f "$WORK"/state/*.json
OUT="$WORK/nochat.out"
if PATH="$WORK/bin:$PATH" \
  FAKE_KUBECTL_MODE=accept \
  FAKE_KUBECTL_STATE="$WORK/state" \
  FAKE_KUBECTL_CALLS="$WORK/calls" \
  FAKE_KUBECTL_WORKLOADS="$WORK/workloads" \
  TELEGRAM_CHAT_ID='' \
  bash "$APPLY" >"$OUT" 2>&1; then
  fail "an absent chat id is refused rather than provisioned empty"
else
  pass "an absent chat id is refused rather than provisioned empty"
fi

# --- a second run over an up-to-date cluster changes nothing ------------------
#
# The applier runs every night. One that restarts Grafana nightly would trade a
# stale configuration for a daily outage of the dashboards.
rm -f "$WORK"/state/*.json
run_apply accept "$WORK/first.out" || true
run_apply accept "$WORK/second.out" || true
if grep -q "rollout restart" "$WORK/calls"; then
  fail "a run over an up-to-date cluster restarts nothing"
else
  pass "a run over an up-to-date cluster restarts nothing"
fi
if grep -qF "unchanged" "$WORK/second.out"; then
  pass "and says the configuration was already current"
else
  fail "and says the configuration was already current (out=[$(cat "$WORK/second.out")])"
fi

# --- a first run restarts each reader once, by the kind it runs as -----------
#
# Every ConfigMap changes on an empty cluster, so every reader is restarted. A
# restart addressed to a kind the workload is not is a NotFound that fails the
# night after the configuration already landed, and the next night reads
# "unchanged" and restarts nothing: the store never reads its new scrape file.
rm -f "$WORK"/state/*.json
if run_apply accept "$WORK/restart.out"; then
  pass "a run that changes every ConfigMap restarts the workloads that read them"
else
  fail "a run that changes every ConfigMap restarts the workloads that read them (out=[$(cat "$WORK/restart.out")])"
fi
for workload in deployment/monitoring-grafana statefulset/monitoring-victoriametrics; do
  n="$(grep -cxF -- "-n monitoring rollout restart $workload" "$WORK/calls" || true)"
  if [ "$n" = "1" ]; then
    pass "$workload is restarted exactly once"
  else
    fail "$workload is restarted exactly once (restarted $n times)"
  fi
done

# --- a ConfigMap is applied whole, and keeps what the cluster put on it --------
#
# Two ways an apply quietly deletes something. One of the three ConfigMaps
# carries a second key that no file in the alerting or dashboard trees produces,
# and rendering only the first is a delete wearing an apply's clothes. And one of
# the three is the chart's: an apply that drops the ownership metadata Helm
# recorded leaves a ConfigMap the next chart upgrade refuses to touch.
rm -f "$WORK"/state/*.json
cat >"$WORK/state/monitoring-victoriametrics-scrape.json" <<'LIVE'
{
  "apiVersion": "v1",
  "kind": "ConfigMap",
  "metadata": {
    "name": "monitoring-victoriametrics-scrape",
    "labels": {"app.kubernetes.io/managed-by": "Helm"},
    "annotations": {
      "meta.helm.sh/release-name": "monitoring",
      "meta.helm.sh/release-namespace": "monitoring"
    }
  },
  "data": {"scrape.yml": "stale", "stream-aggr.yml": "stale"}
}
LIVE
run_apply accept "$WORK/ownership.out" || true

if grep -qF "stream-aggr.yml" <<<"$(python3 -c 'import json,sys; print(" ".join(json.load(open(sys.argv[1]))["data"]))' "$WORK/state/monitoring-victoriametrics-scrape.json" 2>/dev/null)"; then
  pass "the second scrape key survives the apply"
else
  fail "the second scrape key survives the apply"
fi

OWNER="$(python3 -c 'import json,sys; m=json.load(open(sys.argv[1]))["metadata"]; print((m.get("labels") or {}).get("app.kubernetes.io/managed-by",""), (m.get("annotations") or {}).get("meta.helm.sh/release-name",""))' "$WORK/state/monitoring-victoriametrics-scrape.json" 2>/dev/null || printf '')"
if [ "$OWNER" = "Helm monitoring" ]; then
  pass "and the ownership the chart recorded is carried forward"
else
  fail "and the ownership the chart recorded is carried forward (got [$OWNER])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n'
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
