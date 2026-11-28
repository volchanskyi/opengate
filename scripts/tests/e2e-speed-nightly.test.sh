#!/usr/bin/env bash
# Keeps the browser speed specs in their own nightly workflow and out of every other run.
# The split is read from Playwright's own listing of each run, so it holds what the config does.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WEB="$ROOT/web"
NIGHTLY="$ROOT/.github/workflows/e2e-performance.yml"
# The nightly's one job, which the nightly calls as four tries in a row.
NIGHTLY_TRY="$ROOT/.github/workflows/e2e-performance-try.yml"
OTHER_RUNS=(
  "$ROOT/.github/workflows/ci.yml"
  "$ROOT/.github/workflows/e2e-cross-browser.yml"
  "$ROOT/Makefile"
  "$ROOT/scripts/precommit-gauntlet.sh"
)

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

echo "e2e-speed-nightly:"

if [ ! -x "$WEB/node_modules/.bin/playwright" ]; then
  printf '  FAIL Playwright is not installed in web/; run: (cd web && npm ci)\n' >&2
  exit 2
fi

# Prints the spec files one Playwright run reaches, one per line, under the variables given.
spec_files() {
  local listing
  if ! listing="$(cd "$WEB" && env -u PLAYWRIGHT_PERFORMANCE -u PLAYWRIGHT_ALL_BROWSERS "$@" \
    node_modules/.bin/playwright test --list --reporter=list 2>&1)"; then
    printf '%s\n' "$listing" >&2
    return 1
  fi
  sed -nE 's/^  \[[^]]+\] [^ ]+ ([^: ]+\.spec\.ts):[0-9]+:[0-9]+ .*/\1/p' <<<"$listing" | sort -u
}

push_specs="$(spec_files)"
cross_specs="$(spec_files PLAYWRIGHT_ALL_BROWSERS=1)"
speed_specs="$(spec_files PLAYWRIGHT_PERFORMANCE=1)"
all_specs="$(cd "$WEB/e2e" && find . -name '*.spec.ts' -printf '%P\n' | sort)"

if [ -n "$speed_specs" ] && [ -n "$push_specs" ]; then
  pass "the speed run reaches $(wc -l <<<"$speed_specs") spec files, the push run $(wc -l <<<"$push_specs")"
else
  fail "a listing reached no spec file, so the split cannot be read"
fi

for spec in $speed_specs; do
  if grep -qxF -- "$spec" <<<"$push_specs"; then
    fail "$spec runs on every push as well as nightly"
  else
    pass "$spec stays out of the push run"
  fi
  if grep -qxF -- "$spec" <<<"$cross_specs"; then
    fail "$spec runs in the cross-browser nightly as well"
  fi
done

lost=""
for spec in $all_specs; do
  grep -qxF -- "$spec" <<<"$push_specs" || grep -qxF -- "$spec" <<<"$speed_specs" || lost="$lost $spec"
done
if [ -z "$lost" ]; then
  pass "every one of the $(wc -l <<<"$all_specs") spec files reaches one of the two runs"
else
  fail "these spec files reach neither run:$lost"
fi

if [ ! -f "$NIGHTLY" ] || [ ! -f "$NIGHTLY_TRY" ]; then
  fail "missing workflow: $NIGHTLY or $NIGHTLY_TRY"
else
  nightly="$(<"$NIGHTLY")"
  if grep -qF -- "schedule:" <<<"$nightly"; then
    pass "the nightly workflow is scheduled"
  else
    fail "the nightly workflow is not scheduled"
  fi
  nightly_try="$(<"$NIGHTLY_TRY")"
  for needle in "PLAYWRIGHT_PERFORMANCE: '1'" "e2e-stack-up.sh" "npx playwright test" \
    "e2e-summarize.sh performance"; do
    if grep -qF -- "$needle" <<<"$nightly_try"; then
      pass "the nightly's try carries $needle"
    else
      fail "the nightly's try lacks $needle"
    fi
  done
fi

# The nightly runs its one job as four tries in a row, each after a try that refused its runner,
# and its alert waits on all four.
tries="$(
  python3 - "$NIGHTLY" "$NIGHTLY_TRY" <<'PY'
import os, sys

import yaml

nightly, tried = (yaml.safe_load(open(p, encoding="utf-8")) for p in sys.argv[1:3])
try_file = os.path.basename(sys.argv[2])
calls = (tried.get("on") or tried.get(True) or {}).get("workflow_call") or {}
for name in ("refused", "processor"):
    if name not in (calls.get("outputs") or {}):
        print(f"the try hands back no {name}")
jobs = nightly.get("jobs") or {}
for n in range(1, 5):
    job = jobs.get(f"try-{n}") or {}
    if not str(job.get("uses", "")).endswith(f"/{try_file}"):
        print(f"try-{n} does not call {try_file}")
    cond = str(job.get("if", "") or "")
    if n > 1 and ("!cancelled()" not in cond or f"needs.try-{n - 1}.outputs.refused == 'true'" not in cond):
        print(f"try-{n} runs when {cond or 'always'}, where it runs after try-{n - 1} refused its runner")
alert = jobs.get("alert") or {}
if set(alert.get("needs") or []) != {f"try-{n}" for n in range(1, 5)}:
    print(f"the alert waits on {alert.get('needs')}, where it waits on all four tries")
print("ok")
PY
)"
if [ "$tries" = "ok" ]; then
  pass "the nightly runs as four tries in a row, its alert waiting on all four"
else
  fail "tries: $tries"
fi

# The alert step runs here: four refused runners are named, and a passing night sends nothing.
alert_script="$(
  python3 - "$NIGHTLY" <<'PY'
import sys

import yaml

steps = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))["jobs"]["alert"]["steps"]
print(next(s["run"] for s in steps if s.get("name") == "Send Telegram alert"))
PY
)"
ALERT_WORK="$(mktemp -d)"
mkdir -p "$ALERT_WORK/scripts"
cat >"$ALERT_WORK/scripts/telegram-alert.sh" <<EOF
#!/usr/bin/env bash
printf '%s' "\$1" >"$ALERT_WORK/sent"
EOF
chmod +x "$ALERT_WORK/scripts/telegram-alert.sh"
# alert_with REFUSED RESULTS runs the alert step with a send that records its message.
alert_with() {
  rm -f "$ALERT_WORK/sent"
  (cd "$ALERT_WORK" && REFUSED="$1" RESULTS="$2" RUN_URL=https://example.invalid/run \
    PROCESSORS="AMD EPYC 7763 64-Core Processor; AMD EPYC 7763 64-Core Processor; AMD EPYC 7763 64-Core Processor; AMD EPYC 7763 64-Core Processor" \
    bash -c "$alert_script") >"$ALERT_WORK/log" 2>&1 || true
  sent="$(cat "$ALERT_WORK/sent" 2>/dev/null || true)"
}
alert_with true "try-1=success try-2=success try-3=success try-4=success"
if grep -qF "skipped: four runners in a row had a refused processor" <<<"$sent" \
  && grep -qF "AMD EPYC 7763 64-Core Processor" <<<"$sent"; then
  pass "four refused runners alert as a skipped night, naming the processors"
else
  fail "four refused runners alert as a skipped night, naming the processors (sent [$sent])"
fi
alert_with false "try-1=success try-2=skipped try-3=skipped try-4=skipped"
if [ -z "$sent" ]; then
  pass "a night that measured and passed sends nothing"
else
  fail "a night that measured and passed sends nothing (sent [$sent])"
fi
alert_with false "try-1=success try-2=failure try-3=skipped try-4=skipped"
if grep -qF "try-2: failure" <<<"$sent"; then
  pass "a try that failed is alerted by name"
else
  fail "a try that failed is alerted by name (sent [$sent])"
fi
rm -rf "$ALERT_WORK"

# The runner's record brackets the specs: read before they start, and summed after them on every
# outcome, so a red night still says which machine it ran on.
record="$(
  python3 - "$NIGHTLY_TRY" <<'PY'
import sys

import yaml

steps = (yaml.safe_load(open(sys.argv[1], encoding="utf-8"))["jobs"]["e2e"].get("steps") or [])
runs = [str(step.get("run", "") or "") for step in steps]
specs = next((i for i, run in enumerate(runs) if "npx playwright test" in run), None)
before = [i for i, run in enumerate(runs) if "perf-runner-record.sh snapshot" in run and "delta" not in run]
after = [i for i, run in enumerate(runs) if "perf-runner-record.sh delta" in run]
if specs is None:
    print("no step runs the specs")
elif not before or before[0] > specs:
    print("no snapshot is taken before the specs")
elif not after or after[0] < specs:
    print("no record is summed after the specs")
elif "always()" not in str(steps[after[0]].get("if", "")):
    print("the record after the specs runs only on a green run")
elif "GITHUB_STEP_SUMMARY" not in runs[after[0]]:
    print("the record reaches no run summary")
else:
    print("ok")
PY
)"
if [ "$record" = "ok" ]; then
  pass "the runner's record brackets the specs and reaches the summary on every outcome"
else
  fail "runner record: $record"
fi

for entry in "${OTHER_RUNS[@]}"; do
  if grep -qF 'PLAYWRIGHT_PERFORMANCE' "$entry"; then
    fail "$(basename "$entry") switches the speed run on"
  else
    pass "$(basename "$entry") leaves the speed run off"
  fi
done

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '\nFailures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
