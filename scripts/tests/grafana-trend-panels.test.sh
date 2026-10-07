#!/usr/bin/env bash
# A trend panel is an instant query of a bare selector over [$__range], drawn as lines with points.
# Its legend names every label of the measurement; stat panels read last_over_time(…[30d]).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DASHBOARDS="$REPO_ROOT/deploy/grafana/provisioning/dashboards"

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

echo "grafana trend panels:"

# The label table is checked against the pushers, so a label added there and not here fails.
findings="$(
  python3 - "$DASHBOARDS" "$REPO_ROOT/scripts" <<'PY'
import json, pathlib, re, sys

dashboards, scripts = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])

MEASUREMENT_LABELS = [
    (r"^benchmark_", {"benchmark", "lang"}, "benchmark-vm-push.sh"),
    (r"^loadtest_", {"source", "scenario", "phase", "workload"}, "loadtest-vm-push.sh"),
    (r"^netdrill_", {"scenario", "victim"}, "network-drill-vm-push.sh"),
    (r"^mutation_", {"language"}, "mutation-vm-push.sh"),
    (r"^pmat_category_score$", {"category"}, "pmat-vm-push.sh"),
    (r"^pmat_", set(), "pmat-vm-push.sh"),
    (r"^terraform_drift_resources$", {"action", "type"}, "terraform-drift-vm-push.sh"),
    (r"^terraform_drift_", set(), "terraform-drift-vm-push.sh"),
    (r"^perf_", {"leg", "phase", "workload"}, "perf-vm-push.sh"),
]

for pattern, labels, pusher in MEASUREMENT_LABELS:
    text = (scripts / pusher).read_text(encoding="utf-8") if (scripts / pusher).exists() else ""
    if not text:
        print(f"{pusher}: the pusher this table names is not there")
    for label in labels:
        if f"{label}=" not in text:
            print(f"{pusher}: the table says it writes {label}, and it does not")

def labels_of(metric):
    for pattern, labels, _ in MEASUREMENT_LABELS:
        if re.search(pattern, metric):
            return labels
    return None

SELECTOR = re.compile(r'^\{?([a-zA-Z_:][a-zA-Z0-9_:]*)?\{([^}]*)\}\[\$__range\]$')

panels_read = 0
for path in sorted(dashboards.glob("*-trend.json")):
    doc = json.loads(path.read_text(encoding="utf-8"))
    name = path.name
    if not re.search(r"\b(staging|compose stack|CI runner)\b", doc.get("description", "")):
        print(f"{name}: the description does not say where its readings come from")
    for panel in doc.get("panels", []):
        title = panel.get("title", "?")
        kind = panel.get("type")
        for target in panel.get("targets", []):
            expr = target.get("expr", "")
            panels_read += 1
            if kind == "stat":
                if not re.match(r"^last_over_time\([a-z_:]+\{[^}]*env=\"ci\"[^}]*\}\[30d\]\)$", expr):
                    print(f"{name} / {title}: a stat reads last_over_time(metric{{env=\"ci\"}}[30d]), not {expr}")
                continue
            if kind != "timeseries":
                continue
            match = SELECTOR.match(expr)
            if not match or 'env="ci"' not in expr:
                print(f"{name} / {title}: a trend line is a bare selector over [$__range] with env=\"ci\", not {expr}")
                continue
            if not target.get("instant") or target.get("range"):
                print(f"{name} / {title}: the query is not instant, so the store carries readings forward between nights")
            metric = match.group(1)
            if metric:
                labels = labels_of(metric)
            else:
                names = re.search(r'__name__=~"([^"]+)"', match.group(2))
                family = re.sub(r"[^a-z_].*$", "", names.group(1)) if names else ""
                labels = (labels_of(family) or set()) | {"__name__"}
            if labels is None:
                print(f"{name} / {title}: {expr} reads a measurement this table does not know")
                continue
            legend = target.get("legendFormat", "")
            missing = sorted(label for label in labels if "{{" + label + "}}" not in legend.replace(" ", ""))
            if missing:
                print(f"{name} / {title}: the legend [{legend}] does not name {', '.join(missing)}, so two lines can share it")
        if kind == "timeseries":
            custom = panel.get("fieldConfig", {}).get("defaults", {}).get("custom", {})
            if custom.get("drawStyle") != "line" or custom.get("showPoints") != "always":
                print(f"{name} / {title}: a trend draws lines with a point per night")
print(f"READ\t{panels_read}")
PY
)"
read_count="$(sed -n 's/^READ\t//p' <<<"$findings")"
findings="$(grep -v '^READ' <<<"$findings" || true)"
if [ "${read_count:-0}" -eq 0 ]; then
  fail "no trend panel was read, so nothing was checked"
elif [ -z "$findings" ]; then
  pass "all $read_count trend queries draw one line per measurement, a point per night"
else
  while IFS= read -r finding; do
    [ -n "$finding" ] && fail "$finding"
  done <<<"$findings"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
