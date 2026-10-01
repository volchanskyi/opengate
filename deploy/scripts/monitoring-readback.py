"""Ask the running monitoring stack what it loaded and whether what it serves answers.

    loaded <declared-file>          the store's /config on standard input; exits 0
                                    when the process loaded the declared file
    panels <dashboard-dir> [--wait S]
                                    runs every panel's queries against the store;
                                    exits 1 naming each panel that answers nothing
    coverage <alert-rules> [--wait S]
                                    asks the store for every selector a
                                    `watches: production` rule reads; exits 1
                                    naming each that matches nothing

The store is reached through the API server's proxy to its Service, with the
kubectl the caller already holds, so nothing is scheduled to ask it.

A ConfigMap is what a process was given, not what it loaded. The relabel that
names each server series' environment sat in the ConfigMap for two days while
the running store scraped without it, and every production rule read nothing.
So `loaded` compares the process's own account with the declared file, and
`coverage` asks whether each production rule can see anything at all.

Environment:
  VM_NAMESPACE             where the store runs (default monitoring)
  VM_SERVICE               its Service (default monitoring-victoriametrics)
  MONITORING_READBACK_POLL seconds between rounds while waiting (default 10)
"""

import itertools
import json
import os
import pathlib
import re
import subprocess
import sys
import time
import urllib.parse

import yaml

NAMESPACE = os.environ.get("VM_NAMESPACE", "monitoring")
SERVICE = os.environ.get("VM_SERVICE", "monitoring-victoriametrics")
PROXY = f"/api/v1/namespaces/{NAMESPACE}/services/{SERVICE}:8428/proxy"
POLL = float(os.environ.get("MONITORING_READBACK_POLL", "10"))

# The window each kind of dashboard is read over: a live board shows the last
# six hours, a trend board the store's thirty days.
WINDOWS = {"live": 6 * 3600, "trend": 30 * 86400}
RANGE_TEXT = {"live": "6h", "trend": "30d"}
RATE_INTERVAL = {"live": "5m", "trend": "1d"}


class Unreachable(Exception):
    """The store could not be asked at all."""


def ask(path, params):
    url = f"{PROXY}{path}?{urllib.parse.urlencode(params)}"
    done = subprocess.run(
        ["kubectl", "get", "--raw", url], capture_output=True, text=True, timeout=120
    )
    if done.returncode != 0:
        raise Unreachable(" ".join(done.stderr.split()) or f"kubectl exited {done.returncode}")
    try:
        return json.loads(done.stdout)
    except json.JSONDecodeError as err:
        raise Unreachable(f"the store answered something that is not JSON: {err}") from err


# --- loaded -----------------------------------------------------------------


def is_zero(value):
    return value in (None, False, 0, "", [], {})


def covers(live, declared):
    """Whether the loaded configuration is the declared one.

    The store prints what it loaded in its own key order and fills in defaults
    the file leaves out, all of them zero. Anything else it carries, and any
    value that differs, is a different configuration.
    """
    if isinstance(declared, dict):
        if not isinstance(live, dict):
            return False
        if any(key not in live or not covers(live[key], value) for key, value in declared.items()):
            return False
        return all(is_zero(value) for key, value in live.items() if key not in declared)
    if isinstance(declared, list):
        return (
            isinstance(live, list)
            and len(live) == len(declared)
            and all(covers(a, b) for a, b in zip(live, declared))
        )
    return live == declared


def loaded(declared_path):
    declared = yaml.safe_load(pathlib.Path(declared_path).read_text(encoding="utf-8"))
    live = yaml.safe_load(sys.stdin.read())
    return 0 if covers(live, declared) else 1


# --- panels -----------------------------------------------------------------


def panels_of(dashboard):
    for panel in dashboard.get("panels", []):
        if panel.get("type") == "row":
            yield from panel.get("panels", [])
        else:
            yield panel


def variable_sets(dashboard):
    """Every combination of the dashboard's own variable values."""
    choices = []
    for variable in dashboard.get("templating", {}).get("list", []):
        values = [option.get("value") for option in variable.get("options", []) if option.get("value")]
        if values:
            choices.append([(variable["name"], value) for value in values])
    return [dict(combination) for combination in itertools.product(*choices)] or [{}]


def substitute(expr, kind, variables):
    names = dict(variables)
    names.update(
        {
            "__range": RANGE_TEXT[kind],
            "__range_s": str(WINDOWS[kind]),
            "__rate_interval": RATE_INTERVAL[kind],
            "__interval": RATE_INTERVAL[kind],
        }
    )
    for name, value in sorted(names.items(), key=lambda item: -len(item[0])):
        expr = expr.replace("${" + name + "}", value).replace("$" + name, value)
        expr = expr.replace("[[" + name + "]]", value)
    return expr


def answers(expr, instant, kind, now):
    """Whether a query returns anything, or the store's reason it could not run."""
    if instant:
        reply = ask("/api/v1/query", {"query": expr, "time": now})
    else:
        window = WINDOWS[kind]
        step = max(window // 200, 15)
        reply = ask(
            "/api/v1/query_range",
            {"query": expr, "start": now - window, "end": now, "step": step},
        )
    if reply.get("status") != "success":
        return False, reply.get("error", "the query failed")
    return bool(reply.get("data", {}).get("result")), ""


def panel_findings(directory, now):
    findings = []
    for path in sorted(pathlib.Path(directory).glob("*.json")):
        dashboard = json.loads(path.read_text(encoding="utf-8"))
        kind = "trend" if path.name.endswith("-trend.json") else "live"
        for panel in panels_of(dashboard):
            says_empty = bool(panel.get("fieldConfig", {}).get("defaults", {}).get("noValue"))
            for target in panel.get("targets", []) or []:
                expr = target.get("expr")
                if not expr or target.get("hide"):
                    continue
                instant = bool(target.get("instant")) and not target.get("range")
                for variables in variable_sets(dashboard):
                    query = substitute(expr, kind, variables)
                    found, error = answers(query, instant, kind, now)
                    where = f"{dashboard.get('title', path.name)} / {panel.get('title', '?')}"
                    if error:
                        findings.append(f"{where}: the query failed ({error}): {query}")
                    elif not found and not says_empty:
                        findings.append(f"{where}: answers nothing and does not say what empty means: {query}")
    return findings


# --- coverage ---------------------------------------------------------------

SELECTOR = re.compile(r"([a-zA-Z_:][a-zA-Z0-9_:]*)\s*\{([^}]*)\}")
MATCHER = re.compile(r'([a-zA-Z_][a-zA-Z0-9_]*)\s*(=~|!~|!=|=)\s*"([^"]*)"')


def placing_selectors(expr):
    """Each selector reduced to what places its series: the name and the
    exact-match labels. A pattern or a negation picks which outcomes to count,
    and an outcome that has not happened is not a rule that cannot see."""
    for name, body in SELECTOR.findall(expr):
        exact = [f'{label}="{value}"' for label, op, value in MATCHER.findall(body) if op == "="]
        yield f"{name}{{{','.join(exact)}}}"


def coverage_findings(rules_path, now):
    rules = yaml.safe_load(pathlib.Path(rules_path).read_text(encoding="utf-8"))
    findings = []
    for group in rules.get("groups", []):
        for rule in group.get("rules", []):
            if (rule.get("labels") or {}).get("watches") != "production":
                continue
            for query in rule.get("data", []):
                if query.get("datasourceUid") != "VictoriaMetrics":
                    continue
                for selector in placing_selectors(query.get("model", {}).get("expr", "")):
                    reply = ask("/api/v1/series", {"match[]": selector, "start": now - 3600, "end": now})
                    if not reply.get("data"):
                        findings.append(f"{rule.get('uid')}: {selector} matches no series, so the rule reads nothing")
    return findings


def judged(check, target, wait):
    """Runs a check, and runs it again while anything is still missing, up to
    the wait: a store that has just started, or a target just deployed, has not
    taken its first reading yet."""
    deadline = time.time() + wait
    while True:
        findings = check(target, int(time.time()))
        if not findings or time.time() >= deadline:
            return findings
        time.sleep(POLL)


def main(argv):
    if len(argv) >= 2 and argv[0] == "loaded":
        return loaded(argv[1])
    if len(argv) >= 2 and argv[0] in ("panels", "coverage"):
        wait = 0
        if "--wait" in argv:
            wait = float(argv[argv.index("--wait") + 1])
        check = panel_findings if argv[0] == "panels" else coverage_findings
        try:
            findings = judged(check, argv[1], wait)
        except Unreachable as err:
            print(f"::error::the metrics store could not be asked: {err}", file=sys.stderr)
            return 2
        for finding in findings:
            print(f"::error::{finding}", file=sys.stderr)
        return 1 if findings else 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
