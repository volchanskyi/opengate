#!/usr/bin/env bash
# The live dashboards answer for one environment at a time, and say what empty
# means.
#
# Production and staging write into one store and feed one set of dashboards.
# A query that names no environment adds the two together: a load run on
# staging reads as production traffic, and a production panel that should be
# empty — no failed database queries — shows staging's instead. So each live
# dashboard carries an Environment selector, Production by default, and every
# selector names the environment it reads, or the monitoring namespace for the
# monitoring stack's own readings.
#
# A Go program's own readings (goroutines, heap) are published by every Go
# program in a namespace, the database's exporter beside the server included,
# so those name the server's job as well.
#
# Panels for signals the product does not produce are gone, and a panel whose
# query can legitimately come back empty says in words what that means.
#
# Run: ./scripts/tests/grafana-live-dashboards.test.sh
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

echo "grafana live dashboards:"

findings="$(
  python3 - "$DASHBOARDS" <<'PY'
import json, pathlib, re, sys

dashboards = pathlib.Path(sys.argv[1])
LIVE = ["opengate-overview", "db-performance", "postgres", "edge-logs", "edge-sentinel-soak", "rule-rollout"]
WANT_OPTIONS = [("Production", "opengate"), ("Staging", "opengate-staging")]

# Readings the monitoring stack publishes about itself.
MONITORING = re.compile(r"^vm_")
# Readings every Go program publishes, which a namespace alone does not place.
RUNTIME = re.compile(r"^(go|process)_")

KEYWORDS = {"by", "without", "on", "ignoring", "group_left", "group_right", "bool", "and", "or", "unless", "offset", "inf", "nan"}

def selectors(expr):
    """Each series selector in a query, as (metric, matchers)."""
    # Grouping clauses name labels, not series.
    text = re.sub(r"\b(by|without|on|ignoring|group_left|group_right)\s*\([^)]*\)", " ", expr)
    # Range and subquery windows.
    text = re.sub(r"\[[^\]]*\]", " ", text)
    found = []
    for match in re.finditer(r"([a-zA-Z_:][a-zA-Z0-9_:]*)?\s*(\{[^}]*\})?", text):
        name, body = match.group(1), match.group(2)
        if not name and not body:
            continue
        rest = text[match.end():].lstrip()
        if name and rest.startswith("("):
            continue  # a function
        if name and name.lower() in KEYWORDS and not body:
            continue
        if name and re.fullmatch(r"[0-9.e+-]+", name):
            continue
        found.append((name or "", body or ""))
    return found

def panels(doc):
    for panel in doc.get("panels", []):
        if panel.get("type") == "row":
            yield from panel.get("panels", [])
        else:
            yield panel

def no_value(panel):
    return panel.get("fieldConfig", {}).get("defaults", {}).get("noValue", "")

def by_title(doc, title):
    return [p for p in panels(doc) if p.get("title") == title]

read = 0
docs = {}
for name in LIVE:
    path = dashboards / f"{name}.json"
    if not path.exists():
        print(f"{name}: the live dashboard is not there")
        continue
    doc = json.loads(path.read_text(encoding="utf-8"))
    docs[name] = doc

    variables = [v for v in doc.get("templating", {}).get("list", []) if v.get("name") == "environment"]
    if len(variables) != 1:
        print(f"{name}: has no Environment variable")
    else:
        variable = variables[0]
        options = [(o.get("text"), o.get("value")) for o in variable.get("options", [])]
        if variable.get("type") != "custom" or options != WANT_OPTIONS:
            print(f"{name}: the Environment variable offers {options}, not Production (opengate) and Staging (opengate-staging)")
        if (variable.get("current") or {}).get("value") != "opengate":
            print(f"{name}: the Environment variable does not open on Production")
        if variable.get("multi") or variable.get("includeAll"):
            print(f"{name}: the Environment variable lets two environments be added together")

    for panel in panels(doc):
        for target in panel.get("targets", []) or []:
            expr = target.get("expr", "")
            for metric, body in selectors(expr):
                read += 1
                where = f"{name} / {panel.get('title', '?')}: {metric}{body}"
                if MONITORING.search(metric):
                    if 'namespace="monitoring"' not in body:
                        print(f"{where} is the monitoring stack's own reading and must name namespace=\"monitoring\"")
                    continue
                if 'namespace="$environment"' not in body:
                    print(f"{where} names no environment, so production and staging are added together")
                if RUNTIME.search(metric) and 'job="opengate-server"' not in body:
                    print(f"{where} is published by every Go program in the namespace and must name the server's job")

    # Panels do not overlap.
    cells = {}
    for panel in panels(doc):
        g = panel.get("gridPos", {})
        for x in range(g.get("x", 0), g.get("x", 0) + g.get("w", 0)):
            for y in range(g.get("y", 0), g.get("y", 0) + g.get("h", 0)):
                if (x, y) in cells:
                    print(f"{name}: {panel.get('title')} overlaps {cells[(x, y)]}")
                    break
                cells[(x, y)] = panel.get("title")
            else:
                continue
            break

def all_exprs(doc):
    return " ".join(t.get("expr", "") for p in panels(doc) for t in (p.get("targets") or []))

# Signals the product does not produce.
gone = [
    ("opengate-overview", "opengate_signaling_upgrades_total", "the signaling-upgrade counter"),
    ("edge-logs", "log.rate", "host log rates"),
    ("edge-logs", "opengate_edge_metric_avg", "host log rates"),
    ("edge-sentinel-soak", "opengate_edge_family_anomaly_rate", "per-family anomaly rates"),
]
for name, needle, what in gone:
    if name in docs and needle in all_exprs(docs[name]):
        print(f"{name}: still reads {what}, which nothing produces")

# What empty means, in words.
empty_means = [
    ("edge-logs", "Raw-log pull latency p99"),
    ("db-performance", "Error Rate by Operation"),
    ("edge-sentinel-soak", "Anomaly rate (node)"),
    ("edge-sentinel-soak", "Threshold-alert breaches"),
    ("rule-rollout", "Alerts / device / day (measured)"),
]
for name, title in empty_means:
    if name not in docs:
        continue
    found = by_title(docs[name], title)
    if not found:
        print(f"{name}: no panel titled {title}")
    elif not no_value(found[0]):
        print(f"{name} / {title}: can come back empty and does not say what that means")

drift = dashboards / "terraform-drift-trend.json"
if drift.exists():
    drifted = [p for p in panels(json.loads(drift.read_text(encoding="utf-8"))) if "drifted resources" in p.get("title", "").lower()]
    if not drifted or not any(no_value(p) for p in drifted):
        print("terraform-drift-trend: the drifted-resources panel does not say that empty means no drift")

# The relay panel: sessions open, at the highest reading in each step, and
# sessions started per minute, each by pod.
if "opengate-overview" in docs:
    relay = by_title(docs["opengate-overview"], "Relay sessions")
    if not relay:
        print("opengate-overview: no Relay sessions panel")
    else:
        targets = relay[0].get("targets", [])
        exprs = " ".join(t.get("expr", "") for t in targets)
        if "max_over_time(opengate_relay_active_sessions" not in exprs:
            print("opengate-overview / Relay sessions: sessions open must be the highest reading in each step")
        if "opengate_relay_sessions_started_total" not in exprs:
            print("opengate-overview / Relay sessions: does not show sessions started")
        if any("{{pod}}" not in t.get("legendFormat", "").replace(" ", "") for t in targets):
            print("opengate-overview / Relay sessions: every line is named by its pod")
    if by_title(docs["opengate-overview"], "Active Relay Sessions by Replica"):
        print("opengate-overview: the per-replica open count is still there beside the relay panel")

# The exporter splits each connection count by state, user, application and
# wait event, so the stat is their total and the chart one line per state.
if "postgres" in docs:
    for title, want in [("Active Connections", "sum(pg_stat_activity_count"),
                        ("Connections by State", "sum by (state) (pg_stat_activity_count")]:
        exprs = " ".join(t.get("expr", "") for p in by_title(docs["postgres"], title) for t in p.get("targets", []))
        if want not in exprs:
            print(f"postgres / {title}: reads {exprs or 'nothing'}, not {want}…), so it draws one line per user and application")

# The store's size falls when parts merge, so its growth is a delta, never an
# increase that reads each fall as a counter reset.
if "edge-sentinel-soak" in docs:
    if "increase(vm_data_size_bytes" in all_exprs(docs["edge-sentinel-soak"]):
        print("edge-sentinel-soak: the store's growth is an increase over a size that shrinks")
    if "delta(vm_data_size_bytes" not in all_exprs(docs["edge-sentinel-soak"]):
        print("edge-sentinel-soak: the store's growth is not the hourly delta of its size")

print(f"READ\t{read}")
PY
)"
read_count="$(sed -n 's/^READ\t//p' <<<"$findings")"
findings="$(grep -v '^READ' <<<"$findings" || true)"
if [ "${read_count:-0}" -eq 0 ]; then
  fail "no live selector was read, so nothing was checked"
elif [ -z "$findings" ]; then
  pass "all $read_count live selectors name their environment, and every panel answers or says what empty means"
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
