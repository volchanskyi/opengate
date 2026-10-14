#!/usr/bin/env bash
# Sweeps every scheduled workflow for an alert path that survives its own failure.
# Demonstrates the swallowed-status defect first and counts what the sweep reached.
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

    # The names say what the step and the job do, in the words a reader of the
    # run page looks for. The alert send is "Send Telegram alert"; a job that
    # exists only to send it is "Alert if the run did not finish".
    if is_scheduled:
        named_sends = 0
        for job_name, job in jobs.items():
            if not isinstance(job, dict):
                continue
            steps = [s for s in job.get("steps", []) or [] if isinstance(s, dict)]
            sends = [s for s in steps if SENDER in str(s.get("run", "") or "")
                     and "always()" in str(s.get("if", "") or job.get("if", "") or "")]
            named_sends += sum(1 for s in sends if s.get("name") == "Send Telegram alert")
            alert_only = bool(sends) and job.get("needs") and all(
                s in sends or str(s.get("uses", "")).startswith(("actions/checkout", "./.github/actions/setup-pinned-tools"))
                for s in steps
            )
            if alert_only and job.get("name") != "Alert if the run did not finish":
                findings.append(
                    f"{path.name}:{job_name}: a job that exists to send the alert is named "
                    f"[{job.get('name')}] rather than 'Alert if the run did not finish'"
                )
            for s in steps:
                if s.get("name") in ("Say what this night was",):
                    findings.append(f"{path.name}:{job_name}: the send is still named [{s.get('name')}]")
        if found_sender and named_sends == 0:
            findings.append(f"{path.name}: no alert send is named 'Send Telegram alert'")

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

# The Telegram secrets live in a job of their own, so a job that writes the run's summary, or whose
# result is read as its verdict, can neither print them into the page nor fail on a refused send.
cat >"$WORK/held.py" <<'PY'
import pathlib
import re
import sys

import yaml

TELEGRAM = {"DEPLOY_TELEGRAM_BOT_TOKEN", "DEPLOY_TELEGRAM_CHAT_ID"}
TOKEN = "DEPLOY_TELEGRAM_BOT_TOKEN"

reached = 0
for path in sorted(pathlib.Path(sys.argv[1]).glob("*.yml")):
    doc = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    jobs = {name: job for name, job in (doc.get("jobs", {}) or {}).items() if isinstance(job, dict)}
    text = {name: yaml.safe_dump(job) for name, job in jobs.items()}
    verdicts = {m for body in text.values() for m in re.findall(r"needs\.([\w-]+)\.result", body)}
    for name, job in jobs.items():
        secrets = set(re.findall(r"secrets\.([A-Z0-9_]+)", text[name]))
        if not secrets & TELEGRAM:
            continue
        reached += 1
        # The chat names where alerts go and sends nothing, so a job provisioning the destination
        # may hold it; the bot token sends, and its refusal reddens whichever job holds it.
        if TOKEN in secrets and "GITHUB_STEP_SUMMARY" in text[name]:
            print(f"{path.name}:{name}: writes the run summary and holds the bot token")
        if TOKEN in secrets and name in verdicts:
            print(f"{path.name}:{name}: its result is read as a verdict and it holds the bot token")
        if TOKEN in secrets:
            if secrets != TELEGRAM:
                print(f"{path.name}:{name}: sends the alert and holds {sorted(secrets - TELEGRAM)} as well")
            if not job.get("needs") or "always()" not in str(job.get("if", "") or ""):
                print(f"{path.name}:{name}: sends the alert without needs and always()")
print(f"REACHED {reached}")
PY
mkdir -p "$WORK/held-bad" "$WORK/held-good"
cat >"$WORK/held-bad/nightly.yml" <<'YAML'
on: {schedule: [{cron: "0 1 * * *"}]}
jobs:
  publish:
    runs-on: ubuntu-24.04
    steps:
      - run: echo "table" >>"$GITHUB_STEP_SUMMARY"
      - name: Send Telegram alert
        if: always()
        env:
          TELEGRAM_BOT_TOKEN: ${{ secrets.DEPLOY_TELEGRAM_BOT_TOKEN }}
          TELEGRAM_CHAT_ID: ${{ secrets.DEPLOY_TELEGRAM_CHAT_ID }}
        run: scripts/telegram-alert.sh "x"
YAML
cat >"$WORK/held-good/nightly.yml" <<'YAML'
on: {schedule: [{cron: "0 1 * * *"}]}
jobs:
  publish:
    runs-on: ubuntu-24.04
    steps:
      - run: echo "table" >>"$GITHUB_STEP_SUMMARY"
  alert:
    needs: [publish]
    if: always()
    runs-on: ubuntu-24.04
    steps:
      - name: Send Telegram alert
        env:
          RESULT: ${{ needs.publish.result }}
          TELEGRAM_BOT_TOKEN: ${{ secrets.DEPLOY_TELEGRAM_BOT_TOKEN }}
          TELEGRAM_CHAT_ID: ${{ secrets.DEPLOY_TELEGRAM_CHAT_ID }}
        run: scripts/telegram-alert.sh "x"
YAML
bad="$(python3 "$WORK/held.py" "$WORK/held-bad")"
good="$(python3 "$WORK/held.py" "$WORK/held-good")"
if grep -q "writes the run summary" <<<"$bad" && grep -q "without needs and always()" <<<"$bad"; then
  pass "demonstrated: a publish job holding the Telegram secrets is caught"
else
  fail "demonstrated: a publish job holding the Telegram secrets is caught (got=[$bad])"
fi
if [ "$good" = "REACHED 1" ]; then
  pass "demonstrated: an alert job of its own passes"
else
  fail "demonstrated: an alert job of its own passes (got=[$good])"
fi
held="$(python3 "$WORK/held.py" "$WORKFLOWS")"
if grep -qE '^REACHED [1-9]' <<<"$held"; then
  pass "the sweep reached the jobs holding a Telegram secret ($(grep '^REACHED' <<<"$held"))"
else
  fail "the sweep reached no job holding a Telegram secret"
fi
while IFS= read -r finding; do
  [ -n "$finding" ] && fail "$finding"
done < <(grep -v '^REACHED' <<<"$held" || true)

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
