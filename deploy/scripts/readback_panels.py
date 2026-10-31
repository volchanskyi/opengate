"""Every dashboard panel's queries, run against the store over the window its board reads, for
each combination of the board's own variable values."""

import itertools
import json
import pathlib

from readback_store import ask

# A live board reads the last six hours; a trend board reads the store's thirty days.
WINDOWS = {"live": 6 * 3600, "trend": 30 * 86400}
RANGE_TEXT = {"live": "6h", "trend": "30d"}
RATE_INTERVAL = {"live": "5m", "trend": "1d"}


def panels_of(dashboard):
    """Every panel of DASHBOARD, those inside rows among them."""
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
    """EXPR with the board's variables and Grafana's range variables filled in."""
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
    """Each panel query under DIRECTORY that fails, or answers nothing without saying so."""
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
