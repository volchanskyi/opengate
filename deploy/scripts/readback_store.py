"""The metrics store, asked through the API server's proxy to its Service with the kubectl the
caller already holds, so nothing is scheduled to ask it."""

import json
import os
import subprocess
import time
import urllib.parse

NAMESPACE = os.environ.get("VM_NAMESPACE", "monitoring")
SERVICE = os.environ.get("VM_SERVICE", "monitoring-victoriametrics")
PROXY = f"/api/v1/namespaces/{NAMESPACE}/services/{SERVICE}:8428/proxy"
POLL = float(os.environ.get("MONITORING_READBACK_POLL", "10"))


class Unreachable(Exception):
    """The store could not be asked at all."""


def ask(path, params):
    """The store's JSON answer to PATH with PARAMS."""
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
