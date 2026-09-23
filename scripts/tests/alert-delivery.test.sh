#!/usr/bin/env bash
# An alert path is proven by a delivered message, not by a configuration that
# mentions one — swept over every workflow that runs on a schedule.
#
# Two independent channels were dead at once and neither said so. Grafana routed
# every firing rule to a placeholder address, including one that was firing at
# the moment of the check. And across the nightlies, every Telegram step was
# gated on a flag an earlier step in the same job produced, so a night that died
# before that step reported nothing: the louder the failure, the more certain the
# silence. Verified on a publish job that failed at cloud login — the step list
# reads `failure  OCI + kubeconfig setup` then `skipped  Telegram alert`.
#
# Underneath both sat the same shape. Every one of those steps ended its failure
# paths with `exit 0`, so a step that tried to deliver and could not was green.
# The step's conclusion and the delivery were different facts and only one of
# them was ever read.
#
# So this file demonstrates that shape first, and then sweeps for it, counting
# what it reached so a sweep that matched nothing fails instead of passing.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOWS="$REPO_ROOT/.github/workflows"

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "alert delivery:"

# --- the defect, demonstrated -------------------------------------------------
#
# Both shapes are handed the same refused send. The one that swallows its own
# status reports success, which is the whole finding; the one that does not
# reports the refusal. A guard that has stopped reproducing anything fails here
# rather than quietly policing a non-problem.
cat >"$WORK/swallowed.sh" <<'SH'
set -uo pipefail
if ! false; then
  echo "::error::delivery refused"
  exit 0
fi
SH
cat >"$WORK/reported.sh" <<'SH'
set -uo pipefail
if ! false; then
  echo "::error::delivery refused"
  exit 1
fi
SH
if bash "$WORK/swallowed.sh" >/dev/null 2>&1; then
  pass "demonstrated: a send that swallows its status reports a refusal as success"
else
  fail "demonstrated: a send that swallows its status reports a refusal as success"
fi
if bash "$WORK/reported.sh" >/dev/null 2>&1; then
  fail "demonstrated: a send that reports its status fails on a refusal"
else
  pass "demonstrated: a send that reports its status fails on a refusal"
fi

# --- the sweep ----------------------------------------------------------------
#
# Reads each workflow as YAML rather than as lines: a step's condition and the
# condition of the job holding it are both structure, and the shape that hid
# this was a condition, not a string.
python3 - "$WORKFLOWS" "$REPO_ROOT" >"$WORK/sweep.out" 2>"$WORK/sweep.err" <<'PY'
import pathlib
import sys

import yaml

workflows = pathlib.Path(sys.argv[1])
repo = pathlib.Path(sys.argv[2])

SENDER = "scripts/telegram-alert.sh"
APPLIER = "deploy/scripts/monitoring-config-apply.sh"

findings = []
scheduled = 0
senders = 0
applier_callers = []

for path in sorted(workflows.glob("*.yml")):
    doc = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    # `on` is YAML 1.1's boolean true, which is how PyYAML reads an unquoted key.
    triggers = doc.get("on", doc.get(True, {})) or {}
    jobs = doc.get("jobs", {}) or {}
    is_scheduled = isinstance(triggers, dict) and "schedule" in triggers
    if is_scheduled:
        scheduled += 1

    found_sender = False
    for job_name, job in jobs.items():
        if not isinstance(job, dict):
            continue
        job_if = str(job.get("if", "") or "")
        for step in job.get("steps", []) or []:
            if not isinstance(step, dict):
                continue
            run = str(step.get("run", "") or "")
            if APPLIER in run:
                applier_callers.append(f"{path.name}:{job_name}")
            if SENDER not in run:
                continue
            found_sender = True
            senders += 1
            step_if = str(step.get("if", "") or "")
            condition = step_if or job_if
            # An alert conditioned on a healthy run cannot report an unhealthy
            # one. `always()` is what makes the step reachable after the job it
            # reports on has already died.
            if "always()" not in condition:
                findings.append(
                    f"{path.name}:{job_name}: the alert send is conditioned on "
                    f"[{condition or 'nothing'}], which a dead run never reaches"
                )
            # A send that fails takes its own job down with it, which is what
            # makes it honest. So a job that both sends and publishes the run's
            # verdict needs something downstream reading that verdict off
            # `needs.<job>.result` — otherwise a refused send and a clean night
            # are the same colour.
            if job.get("outputs"):
                readers = [
                    other
                    for other, candidate in jobs.items()
                    if isinstance(candidate, dict)
                    and other != job_name
                    and job_name in (candidate.get("needs") or [])
                    and "always()" in str(candidate.get("if", "") or "")
                ]
                if not readers:
                    findings.append(
                        f"{path.name}:{job_name}: publishes the run's verdict and "
                        "sends the alert, and no job reads that verdict after it, "
                        "so a refused send and a clean night are the same colour"
                    )
        for step in job.get("steps", []) or []:
            if not isinstance(step, dict):
                continue
            run = str(step.get("run", "") or "")
            if "api.telegram.org" in run and SENDER not in run:
                findings.append(
                    f"{path.name}:{job_name}: sends to Telegram directly rather "
                    f"than through {SENDER}, so its delivery is judged by a copy"
                )

    if is_scheduled and not found_sender:
        findings.append(
            f"{path.name}: runs on a schedule and has no alert path, so a night "
            "it loses is a night nobody hears about"
        )

if scheduled == 0:
    findings.append("the sweep reached no scheduled workflow at all")
if senders == 0:
    findings.append("the sweep reached no alert send at all")
if not applier_callers:
    findings.append(
        f"no scheduled workflow runs {APPLIER}, so the configuration the "
        "cluster holds is whatever it was last given by hand"
    )

print(f"REACHED\t{scheduled}\t{senders}\t{len(applier_callers)}")
for finding in findings:
    print(f"FINDING\t{finding}")
PY

if [ -s "$WORK/sweep.err" ]; then
  fail "the sweep ran without error (stderr=[$(cat "$WORK/sweep.err")])"
fi

REACHED="$(grep '^REACHED' "$WORK/sweep.out" || printf 'REACHED\t0\t0\t0\n')"
SCHEDULED="$(cut -f2 <<<"$REACHED")"
SENDERS="$(cut -f3 <<<"$REACHED")"
APPLIERS="$(cut -f4 <<<"$REACHED")"

if [ "${SCHEDULED:-0}" -gt 0 ]; then
  pass "the sweep reached $SCHEDULED scheduled workflows"
else
  fail "the sweep reached $SCHEDULED scheduled workflows"
fi
if [ "${SENDERS:-0}" -gt 0 ]; then
  pass "the sweep reached $SENDERS alert sends"
else
  fail "the sweep reached $SENDERS alert sends"
fi
if [ "${APPLIERS:-0}" -gt 0 ]; then
  pass "the monitoring configuration is applied from $APPLIERS scheduled job(s)"
else
  fail "the monitoring configuration is applied from $APPLIERS scheduled job(s)"
fi

FINDINGS="$(grep '^FINDING' "$WORK/sweep.out" | cut -f2- || true)"
if [ -z "$FINDINGS" ]; then
  pass "every scheduled workflow has an alert path that survives its own failure"
else
  while IFS= read -r finding; do
    [ -n "$finding" ] && fail "$finding"
  done <<<"$FINDINGS"
fi

# --- the one send, present and executable -------------------------------------
if [ -x "$REPO_ROOT/scripts/telegram-alert.sh" ]; then
  pass "the single alert send exists and is executable"
else
  fail "the single alert send exists and is executable"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n'
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
