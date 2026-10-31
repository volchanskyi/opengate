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

A ConfigMap is what a process was given. `loaded` compares the process's own
account of what it loaded with the declared file, and `coverage` asks whether
each production rule can see anything at all.

Environment:
  VM_NAMESPACE             where the store runs (default monitoring)
  VM_SERVICE               its Service (default monitoring-victoriametrics)
  MONITORING_READBACK_POLL seconds between rounds while waiting (default 10)
"""

import pathlib
import sys

import yaml

from readback_coverage import coverage_findings
from readback_panels import panel_findings
from readback_store import Unreachable, judged


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
