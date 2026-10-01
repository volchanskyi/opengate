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

# The running store, reached the way the applier reaches it: through the API
# server's proxy to its Service. It answers what it loaded, reloads on request,
# and answers queries from a stand-in store in which every series exists except
# the metrics listed in FAKE_VM_ABSENT.
raw_answer() {
  local path="$1"
  printf 'raw %s\n' "$path" >>"$FAKE_KUBECTL_CALLS"
  case "$path" in
    */proxy/config)
      [ -f "$FAKE_VM_LOADED" ] || {
        echo "Error from server (ServiceUnavailable): no endpoints available" >&2
        return 1
      }
      cat "$FAKE_VM_LOADED"
      ;;
    */proxy/-/reload)
      if [ "${FAKE_VM_RELOAD:-takes}" = "takes" ] && [ -f "$store/monitoring-victoriametrics-scrape.json" ]; then
        python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["scrape.yml"], end="")' \
          "$store/monitoring-victoriametrics-scrape.json" >"$FAKE_VM_LOADED"
      fi
      ;;
    */proxy/api/v1/*)
      python3 - "$path" "${FAKE_VM_ABSENT:-/dev/null}" "$FAKE_VM_QUERIES" <<'STORE'
import json, re, sys, urllib.parse
path, absent_file, log = sys.argv[1], sys.argv[2], sys.argv[3]
query = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
expr = (query.get("query") or query.get("match[]") or [""])[0]
with open(log, "a") as fh:
    fh.write(expr.replace("\n", " ") + "\n")
absent = {line.strip() for line in open(absent_file) if line.strip()}
names = set(re.findall(r"[a-zA-Z_:][a-zA-Z0-9_:]*", expr))
empty = bool(names & absent)
if "/series" in path:
    print(json.dumps({"status": "success", "data": [] if empty else [{"__name__": "x"}]}))
elif "/query_range" in path:
    result = [] if empty else [{"metric": {}, "values": [[0, "1"]]}]
    print(json.dumps({"status": "success", "data": {"resultType": "matrix", "result": result}}))
else:
    result = [] if empty else [{"metric": {}, "value": [0, "1"]}]
    print(json.dumps({"status": "success", "data": {"resultType": "vector", "result": result}}))
STORE
      ;;
    *)
      echo "unexpected raw path: $path" >&2
      return 1
      ;;
  esac
}

case "${args[0]:-}" in
  get)
    if [ "${args[1]:-}" = "--raw" ]; then
      raw_answer "${args[2]:-}"
      exit $?
    fi
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
    if [ "${args[1]:-}" = "status" ]; then
      echo "rollout complete"
      exit 0
    fi
    # A restart names a workload the chart actually runs, by the kind it runs
    # as. Anything else is the NotFound a real cluster answers with. A restarted
    # store starts on what its ConfigMap holds.
    target="${args[2]:-}"
    if [ "$target" = "statefulset/monitoring-victoriametrics" ] && [ -f "$store/monitoring-victoriametrics-scrape.json" ]; then
      python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["scrape.yml"], end="")' \
        "$store/monitoring-victoriametrics-scrape.json" >"$FAKE_VM_LOADED"
    fi
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

SCRAPE_FILE="$REPO_ROOT/deploy/helm/monitoring/files/vmagent-scrape.yaml"
export FAKE_VM_LOADED="$WORK/vm-loaded.yml"
export FAKE_VM_QUERIES="$WORK/vm-queries"

run_apply() {
  local mode="$1" out="$2"
  shift 2
  rm -f "$WORK/calls"
  : >"$WORK/calls"
  : >"$FAKE_VM_QUERIES"
  PATH="$WORK/bin:$PATH" \
    FAKE_KUBECTL_MODE="$mode" \
    FAKE_KUBECTL_STATE="$WORK/state" \
    FAKE_KUBECTL_CALLS="$WORK/calls" \
    FAKE_KUBECTL_WORKLOADS="$WORK/workloads" \
    TELEGRAM_CHAT_ID="-1001234567890" \
    MONITORING_RELOAD_INTERVAL=0 \
    MONITORING_READBACK_WAIT=0 \
    bash "$APPLY" "$@" >"$out" 2>&1
}

# A store that loaded what the repository declares, in the shape VictoriaMetrics
# prints it back: its own key order, and the zero defaults it fills in.
store_loaded_declared() {
  python3 - "$SCRAPE_FILE" >"$FAKE_VM_LOADED" <<'LOADED'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1]))
for job in doc["scrape_configs"]:
    for sd in job.get("kubernetes_sd_configs", []):
        if "namespaces" in sd:
            sd["namespaces"]["own_namespace"] = False
print(yaml.safe_dump(doc, sort_keys=True), end="")
LOADED
}

# stored_key NAME KEY — the content the stub cluster now holds for one key.
stored_key() {
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"].get(sys.argv[2], ""))' \
    "$WORK/state/$1.json" "$2" 2>/dev/null || printf ''
}

echo "monitoring configuration apply:"

# --- an accepting cluster: all three land and read back -----------------------
rm -f "$WORK"/state/*.json
store_loaded_declared
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

# --- the running store is asked what it loaded -------------------------------
#
# The ConfigMap held a relabel for two days that the running store never read:
# a chart upgrade changed the file and restarted nothing, and the nightly apply
# then found the ConfigMap already current and restarted nothing either. Every
# production-scoped rule was blind. So the decision rests on what the process
# loaded, never on what this script changed.
rm -f "$WORK"/state/*.json
run_apply accept "$WORK/seed.out" || true
printf 'global:\n  scrape_interval: 15s\nscrape_configs: []\n' >"$FAKE_VM_LOADED"
OUT="$WORK/stale.out"
if run_apply accept "$OUT"; then
  pass "a store that loaded an older scrape file is brought to the declared one"
else
  fail "a store that loaded an older scrape file is brought to the declared one (out=[$(cat "$OUT")])"
fi
if grep -q -- '/proxy/-/reload' "$WORK/calls" && ! grep -q 'rollout restart' "$WORK/calls"; then
  pass "by a reload, with the ConfigMap already current"
else
  fail "by a reload, with the ConfigMap already current (calls=[$(cat "$WORK/calls")])"
fi

printf 'global:\n  scrape_interval: 15s\nscrape_configs: []\n' >"$FAKE_VM_LOADED"
OUT="$WORK/stuck.out"
if FAKE_VM_RELOAD=ignored run_apply accept "$OUT"; then
  fail "a store still on an older file after reloading is refused"
elif grep -qF '::error::' "$OUT" && grep -qi 'loaded' "$OUT"; then
  pass "a store still on an older file after reloading is refused, and says what it loaded"
else
  fail "a store still on an older file after reloading is refused, and says what it loaded (out=[$(cat "$OUT")])"
fi

# A store that could not be asked is not one that loaded the declared file.
rm -f "$FAKE_VM_LOADED"
OUT="$WORK/unasked.out"
if run_apply accept "$OUT"; then
  fail "a store that cannot be asked what it loaded is refused"
else
  pass "a store that cannot be asked what it loaded is refused"
fi

# --- every panel answers, and every production rule can see ------------------
#
# Against the stand-in store above, where everything exists, the applier runs
# every panel's query and every production rule's selectors.
store_loaded_declared
run_apply accept "$WORK/answers.out" || true
if grep -q 'every panel answers' "$WORK/answers.out" && grep -q 'every production rule' "$WORK/answers.out"; then
  pass "the applier checks the panels and the production rules against the running store"
else
  fail "the applier checks the panels and the production rules against the running store (out=[$(cat "$WORK/answers.out")])"
fi

READBACK="$REPO_ROOT/deploy/scripts/monitoring-readback.py"
readback() {
  PATH="$WORK/bin:$PATH" FAKE_KUBECTL_MODE=accept FAKE_KUBECTL_STATE="$WORK/state" \
    FAKE_KUBECTL_CALLS="$WORK/calls" FAKE_KUBECTL_WORKLOADS="$WORK/workloads" \
    python3 "$READBACK" "$@"
}

mkdir -p "$WORK/dash"
write_dashboard() { # file, title, panel title, expr, noValue, variable json
  python3 - "$@" <<'DASH'
import json, sys
path, title, panel, expr, novalue, variable = sys.argv[1:7]
defaults = {"noValue": novalue} if novalue else {}
templating = {"list": [json.loads(variable)]} if variable else {"list": []}
json.dump({"title": title, "templating": templating, "panels": [{
    "title": panel, "type": "timeseries",
    "datasource": {"type": "prometheus", "uid": "VictoriaMetrics"},
    "fieldConfig": {"defaults": defaults},
    "targets": [{"refId": "A", "expr": expr}]}]}, open(path, "w"))
DASH
}
ENV_VARIABLE='{"name":"environment","type":"custom","options":[{"text":"Production","value":"opengate"},{"text":"Staging","value":"opengate-staging"}]}'
echo "missing_metric" >"$WORK/absent"
rm -f "$WORK/dash"/*
write_dashboard "$WORK/dash/live.json" "Live Board" "Empty Panel" \
  "sum(rate(missing_metric{namespace=\"\$environment\"}[\$__rate_interval]))" "" "$ENV_VARIABLE"
if out="$(FAKE_VM_ABSENT="$WORK/absent" readback panels "$WORK/dash" --wait 0 2>&1)"; then
  fail "a panel whose query answers nothing is refused (out=[$out])"
elif grep -qF 'Live Board' <<<"$out" && grep -qF 'Empty Panel' <<<"$out"; then
  pass "a panel whose query answers nothing is refused, naming the dashboard and the panel"
else
  fail "a panel whose query answers nothing is refused, naming the dashboard and the panel (out=[$out])"
fi

write_dashboard "$WORK/dash/live.json" "Live Board" "Empty Panel" \
  "sum(rate(missing_metric{namespace=\"\$environment\"}[\$__rate_interval]))" "No pulls in this window" "$ENV_VARIABLE"
if out="$(FAKE_VM_ABSENT="$WORK/absent" readback panels "$WORK/dash" --wait 0 2>&1)"; then
  pass "the same panel saying in words what empty means passes"
else
  fail "the same panel saying in words what empty means passes (out=[$out])"
fi

: >"$FAKE_VM_QUERIES"
write_dashboard "$WORK/dash/live.json" "Live Board" "Present Panel" \
  "sum(present_metric{namespace=\"\$environment\"})" "" "$ENV_VARIABLE"
readback panels "$WORK/dash" --wait 0 >/dev/null 2>&1 || true
if grep -qF 'namespace="opengate"}' "$FAKE_VM_QUERIES" && grep -qF 'namespace="opengate-staging"}' "$FAKE_VM_QUERIES"; then
  pass "a live panel is asked once for each environment"
else
  fail "a live panel is asked once for each environment (queries=[$(cat "$FAKE_VM_QUERIES")])"
fi

# The coverage of the production rules: a selector naming a series the store
# does not hold is a rule that cannot see.
cat >"$WORK/rules.yml" <<'RULES'
groups:
  - name: fixture
    rules:
      - uid: blind-production
        title: Blind production rule
        labels: {watches: production}
        data:
          - refId: A
            datasourceUid: VictoriaMetrics
            model: {expr: 'sum(rate(missing_metric{namespace="opengate"}[5m]))'}
      - uid: blind-shared
        title: Shared rule
        labels: {watches: shared}
        data:
          - refId: A
            datasourceUid: VictoriaMetrics
            model: {expr: 'sum(missing_metric{mountpoint="/"})'}
      - uid: outcome-filter
        title: Outcome filter
        labels: {watches: production}
        data:
          - refId: A
            datasourceUid: VictoriaMetrics
            model: {expr: 'sum(rate(present_metric{namespace="opengate",status_code=~"5.."}[5m]))'}
RULES
if out="$(FAKE_VM_ABSENT="$WORK/absent" readback coverage "$WORK/rules.yml" --wait 0 2>&1)"; then
  fail "a production rule whose selector matches nothing is refused (out=[$out])"
else
  if grep -qF 'blind-production' <<<"$out" && grep -qF 'missing_metric{namespace="opengate"}' <<<"$out"; then
    pass "a production rule whose selector matches nothing is refused, naming the rule and the selector"
  else
    fail "a production rule whose selector matches nothing is refused, naming the rule and the selector (out=[$out])"
  fi
  if grep -qF 'blind-shared' <<<"$out"; then
    fail "a shared rule is not held to the production scope"
  else
    pass "a shared rule is not held to the production scope"
  fi
  if grep -qF 'outcome-filter' <<<"$out"; then
    fail "an outcome picked by a pattern is left to the data"
  else
    pass "an outcome picked by a pattern is left to the data"
  fi
fi

# What the store loaded, compared as structure: its own key order and the zero
# defaults it fills in are the same configuration; a changed value is not.
printf 'a: 1\nb: {c: x}\n' >"$WORK/declared.yml"
if readback loaded "$WORK/declared.yml" <<<$'b:\n  c: x\n  d: false\na: 1'; then
  pass "a loaded configuration in another order with zero defaults is the declared one"
else
  fail "a loaded configuration in another order with zero defaults is the declared one"
fi
if readback loaded "$WORK/declared.yml" <<<$'a: 1\nb: {c: y}'; then
  fail "a loaded configuration with a different value is not the declared one"
else
  pass "a loaded configuration with a different value is not the declared one"
fi
if readback loaded "$WORK/declared.yml" <<<$'a: 1\nb: {c: x, relabel: keep}'; then
  fail "a loaded configuration carrying something the file does not declare is not the declared one"
else
  pass "a loaded configuration carrying something the file does not declare is not the declared one"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n'
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
