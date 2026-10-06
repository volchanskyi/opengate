#!/usr/bin/env bash
# The endurance job offers the technician load its profile declares and folds the numbers in.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOW="$REPO_ROOT/.github/workflows/soak.yml"
PROFILE="$REPO_ROOT/load/profiles/soak.yaml"
SCENARIO_DIR="$REPO_ROOT/load/k6/scenarios"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$REPO_ROOT/scripts/lib/loadtest-profile.sh"

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

echo "soak workflow:"

for f in "$WORKFLOW" "$PROFILE"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

workflow="$(cat "$WORKFLOW")"

if ! profile_reader_available; then
  fail "this machine cannot read a profile (python3 with PyYAML), so what the endurance run declares could not be asked for"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

walk="$(profile_phases "$PROFILE")"
declared_arrivals="$(jq '[.[] | select(.arrivals_per_second > 0)] | length' <<<"$walk")"
declared_sessions="$(jq '[.[] | select(.sessions > 0)] | length' <<<"$walk")"

if [ "$declared_arrivals" -gt 0 ]; then
  pass "the profile declares technician arrivals in $declared_arrivals phase(s)"
else
  fail "the profile declares no technician arrivals, so the checks below hold it to nothing"
fi
if [ "$declared_sessions" -gt 0 ]; then
  pass "the profile declares held sessions in $declared_sessions phase(s)"
else
  fail "the profile declares no sessions, so the operation this family's leak attaches to is not in its shape"
fi

alongside="$(grep -oE 'loadtest-k6-alongside\.sh[^&]*' <<<"$workflow" | head -1 || true)"
if [ -n "$alongside" ]; then
  pass "the endurance job runs a browser-side generator beside the walk"
else
  fail "the endurance job offers no technician load, so the numbers its profile declares describe an intention rather than a fact"
fi

scenarios=()
read -r -a alongside_words <<<"$alongside"
for word in "${alongside_words[@]:2}"; do
  case "$word" in
    -* | '' | '&' | \\) continue ;;
    *) scenarios+=("$word") ;;
  esac
done

if [ "${#scenarios[@]}" -gt 0 ]; then
  pass "the leg names ${#scenarios[@]} scenario(s)"
else
  fail "the leg names no scenario, so it starts a generator with nothing to offer"
fi

# A scenario's executor says which number it offers: arrival journeys or held sessions.
offers_arrivals=no
offers_sessions=no
for scenario in "${scenarios[@]}"; do
  script="$SCENARIO_DIR/$scenario.js"
  if [ ! -f "$script" ]; then
    fail "the leg names scenario $scenario and there is no such script"
    continue
  fi
  grep -q 'arrivalScenarios' "$script" && offers_arrivals=yes
  grep -q 'sessionScenarios' "$script" && offers_sessions=yes
done

if [ "$declared_arrivals" -eq 0 ] || [ "$offers_arrivals" = yes ]; then
  pass "the technician arrivals the profile declares are offered"
else
  fail "the profile declares technician arrivals and no scenario on the leg offers a rate of journeys"
fi
if [ "$declared_sessions" -eq 0 ] || [ "$offers_sessions" = yes ]; then
  pass "the sessions the profile declares are held open"
else
  fail "the profile declares sessions and no scenario on the leg holds any open, so the operation this family's leak attaches to never happens"
fi

if grep -q -- '-relay-sessions' <<<"$workflow"; then
  pass "the harness joins the machine side of a session and echoes"
else
  fail "the harness is not told to answer a session request, so the browser side times a frame nobody sends back and no session enters the conservation denominator"
fi

if grep -q 'grafana/k6/releases/download' <<<"$workflow" \
  && grep -qE '^[[:space:]]*K6_VERSION:' <<<"$workflow"; then
  pass "the job fetches a pinned build of the generator it runs"
else
  fail "the job runs a browser-side generator it never installs"
fi

folded=0
for scenario in "${scenarios[@]}"; do
  if grep -q -- "--journeys[^|]*$scenario\.json" <<<"$workflow"; then
    folded=$((folded + 1))
    pass "$scenario's numbers are folded into the run's own evidence"
  else
    fail "$scenario is offered and its numbers are folded nowhere, so they reach a temp directory and stop"
  fi
done

while IFS= read -r export_name; do
  [ -n "$export_name" ] || continue
  runs=no
  for scenario in "${scenarios[@]}"; do
    [ "$scenario" = "$export_name" ] && runs=yes
  done
  if [ "$runs" = yes ]; then
    pass "the export folded for $export_name is one the leg produces"
  else
    fail "the job folds $export_name's export and the leg never runs it"
  fi
done < <(grep -oE '\-\-journeys [^ ]+' <<<"$workflow" \
  | sed -nE 's|.*/([a-z0-9-]+)\.json.*|\1|p' | sort -u)

if [ "$folded" -gt 0 ]; then
  pass "read $folded folded scenario(s) off the job"
else
  fail "no folded scenario was read, so this sweep checked nothing"
fi

# The core holds the fixture's data and the repository is public, so only an encrypted dump leaves.
dump_facts="$(
  python3 - "$WORKFLOW" <<'PY_DUMP'
import json, sys, yaml

doc = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
job = doc["jobs"]["soak"]
env = {**doc.get("env", {}), **job.get("env", {})}
steps = job["steps"]
names = [s.get("name", s.get("uses", "")) for s in steps]

def index_of(pred):
    for i, s in enumerate(steps):
        if pred(s):
            return i
    return -1

check = index_of(lambda s: "--check-recipient" in s.get("run", ""))
walk = index_of(lambda s: "loadtest-quic-run.sh" in s.get("run", ""))
reference = index_of(lambda s: "loadtest-reference-walk.sh opengate" in s.get("run", ""))
uploads = [s for s in steps if str(s.get("uses", "")).startswith("actions/upload-artifact")]
print(json.dumps({
    "recipient_env": env.get("SOAK_DUMP_AGE_RECIPIENT", ""),
    "check_before_walk": check != -1 and walk != -1 and check < walk,
    "reference_run": steps[reference].get("run", "") if reference != -1 else "",
    "bundle_dir": env.get("SOAK_BUNDLE_DIR", ""),
    "walk_dir": env.get("SOAK_WALK_DIR", ""),
    "dump_dir": env.get("SOAK_DUMP_DIR", ""),
    "uploads": [{"name": u["with"].get("name"), "path": u["with"].get("path"),
                 "missing": u["with"].get("if-no-files-found")} for u in uploads],
    "installs": "\n".join(s.get("run", "") for s in steps),
}))
PY_DUMP
)"
fact() { jq -r "$1" <<<"$dump_facts"; }

if [ "$(fact .recipient_env)" = "\${{ secrets.SOAK_DUMP_AGE_RECIPIENT }}" ]; then
  pass "the soak job hands the walk the maintainer's public key from the repository secret"
else
  fail "the soak job hands the walk the maintainer's public key from the repository secret (got=[$(fact .recipient_env)])"
fi
if [ "$(fact .check_before_walk)" = "true" ]; then
  pass "the recipient is checked before the five-hour walk rather than after it"
else
  fail "the recipient is checked before the five-hour walk rather than after it"
fi
dump_dir="$(fact .dump_dir)"
bundle_dir="$(fact .bundle_dir)"
if [ -n "$dump_dir" ] && grep -qF -- "\"\$SOAK_DUMP_DIR\"" <<<"$(fact .reference_run)"; then
  pass "the reference walk is told where the encrypted dump goes"
else
  fail "the reference walk is told where the encrypted dump goes (run=[$(fact .reference_run)])"
fi
case "$dump_dir/" in
  "$bundle_dir"/*) fail "the encrypted dump is written inside the bundle ($dump_dir)" ;;
  *) pass "the encrypted dump is written apart from the bundle" ;;
esac

plain=0
dump_upload=""
while IFS=$'\t' read -r name path missing; do
  if grep -qE 'core\.|RUNNER_TEMP|runner\.temp' <<<"$path"; then
    plain=$((plain + 1))
    fail "the $name upload names a plain dump path ($path)"
  fi
  if [ "$path" = "\${{ env.SOAK_DUMP_DIR }}" ]; then
    dump_upload="$name $missing"
  fi
done < <(jq -r '.uploads[] | [.name, .path, (.missing // "")] | @tsv' <<<"$dump_facts")
if [ "$plain" -eq 0 ]; then
  pass "no upload names a plain dump path"
fi
if [ "${dump_upload#* }" = "error" ]; then
  pass "the encrypted dump is uploaded as an artifact of its own, which fails when it is empty"
else
  fail "the encrypted dump is uploaded as an artifact of its own, which fails when it is empty (got=[$dump_upload])"
fi

installs="$(fact .installs)"
if grep -qF 'scripts/install-viewcore.sh' <<<"$installs" \
  && ! grep -qE 'go install [^ ]*viewcore' <<<"$installs"; then
  pass "the reader is installed patched, from the manifest's pin"
else
  fail "the reader is installed patched, from the manifest's pin"
fi
if grep -qF 'scripts/install-dump-tools.sh' <<<"$installs"; then
  pass "the job installs the manifest's age and zstd"
else
  fail "the job installs the manifest's age and zstd"
fi

TREND_WORKFLOW="$WORKFLOW"
publish_problems="$(
  python3 - "$TREND_WORKFLOW" <<'PY_PUBLISH'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
jobs = doc["jobs"]
publish = jobs.get("publish")
if not publish:
    print("there is no publish job")
    sys.exit(0)
runs = "\n".join(str(s.get("run", "")) for s in publish.get("steps", []))
env = publish.get("env") or {}
if "scripts/perf-vm-push.sh" not in runs:
    print("the publish job does not push the legs' rows")
if "scripts/perf-regression-check.sh" not in runs:
    print("the publish job does not compare the legs with the nights before them")
if "outputs.started_at" not in str(env.get("VM_RUN_STARTED_AT", "")):
    print("the publish job is not handed the time the run started")
if "always()" not in str(publish.get("if", "")):
    print("the publish job does not run after a leg that failed")
if not any("oci-kube-setup" in str(s.get("uses", "")) for s in publish.get("steps", [])):
    print("the publish job never reaches the store")
gate = jobs.get("gate") or {}
gate_runs = "\n".join(str(s.get("run", "")) for s in gate.get("steps", []))
if "publish" not in (gate.get("needs") or []) or "always()" not in str(gate.get("if", "")):
    print("no gate job reads the publish job whatever happened to it")
if "needs.publish.result" not in gate_runs or "needs.publish.outputs.regression" not in gate_runs:
    print("the gate does not read the publish job's result and its finding")
PY_PUBLISH
)"
if [ -z "$publish_problems" ]; then
  pass "$(basename "$TREND_WORKFLOW") publishes its legs and gates them against their nights"
else
  fail "$(basename "$TREND_WORKFLOW"): $publish_problems"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
