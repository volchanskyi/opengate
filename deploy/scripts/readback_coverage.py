"""Every selector a production alert rule reads, asked of the store, so a rule that can see
nothing at all is named."""

import pathlib
import re

import yaml

from readback_store import ask

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
    """Each production rule's selector that matches no series in the last hour."""
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
