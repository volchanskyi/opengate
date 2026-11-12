#!/usr/bin/env bash
# The runner-hosted performance stack must be able to host the load it is for.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE="$REPO_ROOT/deploy/docker-compose.perf.yml"
WORKFLOWS_DIR="$REPO_ROOT/.github/workflows"
LEG_WORKFLOW="$WORKFLOWS_DIR/perf-leg.yml"
TREND_WORKFLOW="$WORKFLOWS_DIR/perf-trend.yml"
# The four domains, each a nightly of its own over the shared leg and the shared trend.
DOMAINS=("$WORKFLOWS_DIR/perf-scaling.yml" "$WORKFLOWS_DIR/perf-volume.yml" "$WORKFLOWS_DIR/perf-shapes.yml"
  "$WORKFLOWS_DIR/perf-catchup.yml")
PERF_WORKFLOWS=("$LEG_WORKFLOW" "$TREND_WORKFLOW" "${DOMAINS[@]}")
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

echo "perf stack:"

for f in "$COMPOSE" "${PERF_WORKFLOWS[@]}"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

# A machine reaches the server over QUIC, which is UDP.
if grep -qE '^\s*-\s*"9090:9090/udp"' "$COMPOSE"; then
  pass "the machine-facing QUIC port is published"
else
  fail "the machine-facing QUIC port is published"
fi

for service in postgres server metrics; do
  if awk -v svc="$service" '
      $0 ~ "^  " svc ":" { in_svc = 1; next }
      /^  [a-z-]+:$/ { in_svc = 0 }
      in_svc && /cpus:/ { found = 1 }
      END { exit !found }
    ' "$COMPOSE"; then
    pass "$service declares a processor bound"
  else
    fail "$service declares a processor bound"
  fi
  if awk -v svc="$service" '
      $0 ~ "^  " svc ":" { in_svc = 1; next }
      /^  [a-z-]+:$/ { in_svc = 0 }
      in_svc && /memory:/ { found = 1 }
      END { exit !found }
    ' "$COMPOSE"; then
    pass "$service declares a memory bound"
  else
    fail "$service declares a memory bound"
  fi
done

if grep -q 'PERF_SERVER_CPUS' "$COMPOSE"; then
  pass "the server's processor count is the sweep's variable"
else
  fail "the server's processor count is the sweep's variable"
fi

# The volume family measures disk a fixture occupies, so the database data stays off tmpfs.
postgres_block() {
  awk '
    /^  postgres:/ { in_svc = 1; next }
    /^  [a-z-]+:$/ { in_svc = 0 }
    in_svc { print }
  ' "$COMPOSE"
}
# Read once into a variable: under pipefail a `grep -q` that stops early fails the writer's pipe.
POSTGRES_BLOCK="$(postgres_block)"
if grep -qE '^[[:space:]]+tmpfs:' <<<"$POSTGRES_BLOCK"; then
  fail "the database writes to tmpfs, so a fixture would have no measurable size"
elif grep -qF 'perf-postgres:/var/lib/postgresql/data' <<<"$POSTGRES_BLOCK"; then
  pass "the database writes to a volume, so a fixture has a measurable size"
else
  fail "the database names no data volume"
fi

if grep -q 'victoria-metrics' "$COMPOSE"; then
  pass "the stack runs a metrics store"
else
  fail "the stack runs a metrics store"
fi

if grep -q 'OPENGATE_TEST_MODE' "$COMPOSE"; then
  fail "the perf stack sets OPENGATE_TEST_MODE, which no Go source reads"
else
  pass "the perf stack sets no variable the server does not read"
fi

if command -v docker >/dev/null 2>&1; then
  if DOCKER_CONFIG="$("$REPO_ROOT/scripts/docker-credstore-guard.sh")" \
    docker compose -f "$COMPOSE" config >/dev/null 2>&1; then
    pass "compose parses the perf stack"
  else
    fail "compose parses the perf stack"
  fi
else
  echo "  note docker not on PATH; compose parse not exercised here (CI runs it)"
fi

# Compose's `${VAR:-default}` also replaces a variable that is set and empty;
# `${VAR-default}` keeps the empty value a workflow sets on purpose.
emptied=()
while IFS= read -r name; do
  [ -n "$name" ] && emptied+=("$name")
done < <(
  grep -rhoE "^[[:space:]]+[A-Z][A-Z0-9_]*:[[:space:]]*''[[:space:]]*$" "$REPO_ROOT"/.github/workflows/*.yml \
    | sed -E "s/^[[:space:]]+([A-Z0-9_]+):.*/\1/" | sort -u
)

if [ "${#emptied[@]}" -gt 0 ] && grep -qxF 'PERF_SERVER_GO_LDFLAGS' <<<"$(printf '%s\n' "${emptied[@]}")"; then
  pass "the sweep read the variables a workflow empties on purpose (${#emptied[@]})"
else
  fail "no workflow empties a variable on purpose, so this sweep is checking nothing"
fi

discarded=""
for name in "${emptied[@]}"; do
  if grep -qF "\${$name:-" "$COMPOSE"; then
    discarded="$discarded $name"
  fi
done
if [ -z "$discarded" ]; then
  pass "an emptied variable reaches the build rather than being replaced by a default"
else
  fail "compose reads these with \${VAR:-default}, which discards the empty value the workflow set:$discarded"
fi

if command -v docker >/dev/null 2>&1; then
  rendered="$(
    PERF_SERVER_GO_LDFLAGS='' DOCKER_CONFIG="$("$REPO_ROOT/scripts/docker-credstore-guard.sh")" \
      docker compose -f "$COMPOSE" config 2>/dev/null || true
  )"
  built_with="$(sed -n 's/^ *GO_LDFLAGS: *//p' <<<"$rendered" | head -1)"
  if [ -z "$rendered" ]; then
    fail "compose rendered nothing, so what the endurance target is linked with was not read back"
  elif [ "$built_with" = '""' ] || [ -z "$built_with" ]; then
    pass "the endurance target is built with the empty link the soak asks for"
  else
    fail "the endurance target is built with [$built_with], not the empty link the soak asks for"
  fi
else
  echo "  note docker not on PATH; what compose renders is not read back here (CI runs it)"
fi

# legs prints one line a leg every domain calls the shared leg for, its matrix expanded:
# domain, job, leg, profile, catch-up, weigh, technician allowance and arm, separated by tabs.
legs() {
  python3 - "$LEG_WORKFLOW" "${DOMAINS[@]}" <<'PY_LEGS'
import itertools, os, re, sys

import yaml

leg_file, domains = os.path.basename(sys.argv[1]), sys.argv[2:]
expr = re.compile(r"\$\{\{\s*matrix\.(\w+)(?:\s*\|\|\s*'([^']*)')?\s*\}\}")


def combos(matrix):
    if not matrix:
        return [{}]
    if "include" in matrix:
        return list(matrix["include"])
    keys = list(matrix)
    return [dict(zip(keys, values)) for values in itertools.product(*(matrix[k] for k in keys))]


def resolve(value, combo):
    text = str(value)
    return expr.sub(lambda m: str(combo.get(m.group(1), m.group(2) or "")), text)


for path in domains:
    doc = yaml.safe_load(open(path, encoding="utf-8"))
    for job_name, job in (doc.get("jobs") or {}).items():
        if not str(job.get("uses", "")).endswith(f"/{leg_file}"):
            continue
        for combo in combos((job.get("strategy") or {}).get("matrix")):
            given = {k: resolve(v, combo) for k, v in (job.get("with") or {}).items()}
            print("\t".join([os.path.basename(path), job_name, given.get("leg", ""), given.get("profile", ""),
                             given.get("catch-up", "False"), given.get("weigh", "False"),
                             given.get("technician-cpus", ""), given.get("arm", "False")]))
PY_LEGS
}

LEGS="$(legs)"
if [ -n "$LEGS" ]; then
  pass "the domains call the shared leg for $(wc -l <<<"$LEGS") leg(s)"
else
  fail "no domain calls the shared leg, so every check below reads nothing"
fi

# A leg's name is its trend series, so no two legs share one.
dupes="$(cut -f3 <<<"$LEGS" | sort | uniq -d)"
if [ -z "$dupes" ]; then
  pass "every leg has a name of its own, which its trend series carries"
else
  fail "legs share a name, so their nights mix in one series: $dupes"
fi

walked_profiles="$(cut -f4 <<<"$LEGS")"
for family in volume- scaling peak spike breakpoint catch-up; do
  if grep -q "load/profiles/${family}" <<<"$walked_profiles"; then
    pass "a domain walks the ${family%-} profile"
  else
    fail "no domain walks the ${family%-} profile"
  fi
done

# The volume legs weigh their fixture, and the catch-up leg alone runs the store probe on ARM.
if awk -F'\t' '$4 ~ /volume-/ && $6 != "True" { bad = 1 } END { exit bad }' <<<"$LEGS"; then
  pass "every volume leg weighs the fixture it built"
else
  fail "a volume leg does not weigh the fixture it built"
fi
if awk -F'\t' '($3 == "catch-up") != ($5 == "True" && $8 == "True") { bad = 1 } END { exit bad }' <<<"$LEGS"; then
  pass "the catch-up leg alone runs the store probe, on ARM"
else
  fail "the store probe or the ARM runner reaches a leg other than catch-up"
fi

missing_paths="$(
  python3 - "$REPO_ROOT" "${PERF_WORKFLOWS[@]}" <<'PY'
import os
import re
import sys

import yaml

root = sys.argv[1]
candidate = re.compile(r"(?<![\w/.-])((?:scripts|load|deploy|policy|benchmarks)/[\w./-]+)")
missing = []
for workflow in sys.argv[2:]:
    with open(workflow, encoding="utf-8") as handle:
        document = yaml.safe_load(handle)
    texts = []
    for job in document.get("jobs", {}).values():
        job_dir = (job.get("defaults", {}).get("run", {}) or {}).get("working-directory", "")
        texts += [(str(v), "") for v in (job.get("with") or {}).values()]
        texts += [(str(v), "") for v in (job.get("env") or {}).values()]
        for step in job.get("steps", []) or []:
            if step.get("run"):
                texts.append((step["run"], step.get("working-directory", job_dir) or ""))
    for script, step_dir in texts:
        for path in set(candidate.findall(script)):
            if path.endswith((".", "/")):
                continue
            if not os.path.exists(os.path.normpath(os.path.join(root, step_dir, path))):
                missing.append(f"{os.path.basename(workflow)}: {path} (from {step_dir or '.'})")
for row in sorted(missing):
    print(row)
PY
)"
if [ -z "$missing_paths" ]; then
  pass "every path a domain or the leg names exists from where it is read"
else
  fail "paths named from the wrong directory: $missing_paths"
fi

# Numeric readings live in the metrics store, so the weighing counts only tables Postgres has.
weighed_tables="$(grep -oE 'table_rows [a-z_]+' "$REPO_ROOT/scripts/perf-weigh-fixture.sh" | awk '{ print $2 }' | sort -u)"
unknown_tables=""
for table in $weighed_tables; do
  if ! grep -rqE "CREATE TABLE IF NOT EXISTS $table |ALTER TABLE [a-z_]+ RENAME TO $table;" \
    "$REPO_ROOT/server/internal/db/migrations/"; then
    unknown_tables="$unknown_tables $table"
  fi
done
if [ -z "$unknown_tables" ]; then
  pass "the weighing counts only tables the schema creates"
else
  fail "the weighing counts tables that do not exist:$unknown_tables"
fi

# The leg's walk: the server signs the harness's certificates, the fleet is the profile's, the
# technicians walk the harness's profile, and the harness answers the sessions they open.
walk_problems="$(
  python3 - "$LEG_WORKFLOW" <<'PY_WALK'
import re, sys, yaml

job = next(iter(yaml.safe_load(open(sys.argv[1], encoding="utf-8"))["jobs"].values()))
steps = job.get("steps") or []
walk = next((s for s in steps if "/tmp/loadtest" in str(s.get("run", ""))), None)
if walk is None:
    print("the leg runs no harness")
    sys.exit(0)
run, env = str(walk["run"]), {**(job.get("env") or {}), **(walk.get("env") or {})}
for flag, why in (("-enroll-url=", "the harness signs its own certificates against an authority the server never made"),
                  ("-fixture-account=", "the walk measures against an empty database"),
                  ("-relay-sessions", "nobody answers a session a technician opens"),
                  ('-profile="$PROFILE"', "the harness walks something other than the leg's profile")):
    if flag not in run:
        print(why)
if str(env.get("LOADTEST_PROFILE", "")) != str(env.get("PROFILE", "")) or "inputs.profile" not in str(env.get("PROFILE", "")):
    print(f"the technicians walk {env.get('LOADTEST_PROFILE')} and the harness walks {env.get('PROFILE')}")
k6 = re.search(r"loadtest-k6-alongside\.sh\s+\S+\s+([^&\n]+)&", run)
if not k6:
    print("the leg starts no browser-side generator")
    sys.exit(0)
scenarios = k6.group(1).split()
folds = "\n".join(str(s.get("run", "")) for s in steps if "--journeys" in str(s.get("run", "")))
for scenario in scenarios:
    if f"--journeys /tmp/perf-k6/{scenario}.json" not in folds:
        print(f"the leg offers {scenario} and folds its numbers nowhere")
for folded in re.findall(r"--journeys /tmp/perf-k6/([a-z0-9-]+)\.json", folds):
    if folded not in scenarios:
        print(f"the leg folds {folded}'s export and never runs it")
print("scenarios=" + " ".join(scenarios))
PY_WALK
)"
SCENARIOS="$(sed -n 's/^scenarios=//p' <<<"$walk_problems")"
walk_problems="$(grep -v '^scenarios=' <<<"$walk_problems" || true)"
if [ -z "$walk_problems" ] && [ -n "$SCENARIOS" ]; then
  pass "the leg's harness and technicians walk one profile against the server's authority, and fold what they ran"
else
  fail "the leg's walk:"$'\n'"$walk_problems"
fi

# Every leg's profile declares a technician load, and its declared sessions are held open by a
# scenario the leg runs.
offers_sessions=no
for scenario in $SCENARIOS; do
  script="$REPO_ROOT/load/k6/scenarios/$scenario.js"
  if [ ! -f "$script" ]; then
    fail "the leg names scenario $scenario and there is no such script"
    continue
  fi
  grep -q 'sessionScenarios' "$script" && offers_sessions=yes
done
legs_read=0
while IFS=$'\t' read -r domain _ leg profile _; do
  [ -n "$leg" ] || continue
  if [ -z "$profile" ] || [ ! -f "$REPO_ROOT/$profile" ]; then
    fail "$domain's $leg leg names [$profile], which is no profile"
    continue
  fi
  legs_read=$((legs_read + 1))
  phases="$(profile_phases "$REPO_ROOT/$profile" 2>/dev/null)" || {
    fail "$domain's $leg leg names $profile, which does not read"
    continue
  }
  declares="$(jq '[.[] | select(.sessions > 0 or .arrivals_per_second > 0)] | length' <<<"$phases")"
  sessions="$(jq '[.[] | select(.sessions > 0)] | length' <<<"$phases")"
  if [ "$declares" -eq 0 ]; then
    fail "$domain's $leg leg walks a profile with no technician load, so its claim is about an idle fleet"
  elif [ "$sessions" -gt 0 ] && [ "$offers_sessions" != yes ]; then
    fail "$domain's $leg leg declares sessions and no scenario the leg runs holds one open"
  else
    pass "$domain's $leg leg offers the technician load its profile declares"
  fi
done <<<"$LEGS"
if [ "$legs_read" -eq 0 ]; then
  fail "no leg was read, so every check above asserted an absence it never tested"
fi

# A failed leg keeps the machine it ran on, and the store's readings reach the uploaded bundle
# whatever became of the server after the probe.
leg_problems="$(
  python3 - "$LEG_WORKFLOW" <<'PY_LEG'
import re
import sys

import yaml

steps = next(iter(yaml.safe_load(open(sys.argv[1], encoding="utf-8"))["jobs"].values()))["steps"]
runs = [str(s.get("run", "")) for s in steps]
conds = [str(s.get("if", "") or "") for s in steps]
record = [i for i, run in enumerate(runs) if "perf-runner-record.sh delta" in run]
if not record:
    print("no step sums the runner's record")
else:
    if "always()" not in conds[record[0]] or "steps.stack.outcome == 'success'" not in conds[record[0]]:
        print(f"the record runs only on a leg that passed ({conds[record[0]] or 'no if'})")
    if "--server-stopped" not in runs[record[0]]:
        print("the runner's record has no reading for a server that stopped")
if not any("verdict" in run for run in runs):
    print("the leg never reads the bundle's verdict, so a run that measured nothing passes")
fold = [i for i, run in enumerate(runs) if "--probe" in run]
judge = next((i for i, run in enumerate(runs) if "perf-bundle-limits.sh" in run), None)
if len(fold) != 1:
    print(f"{len(fold)} steps fold the store's readings in, where one does")
else:
    run, condition = runs[fold[0]], conds[fold[0]]
    path = re.search(r'--probe\s+"?([^"\s]+)"?', run).group(1)
    if not path.startswith("$PERF_BUNDLE_DIR/"):
        print(f"the store's readings sit at {path}, outside the uploaded bundle directory")
    elif not any(path in r for r in runs[: fold[0]] if "jq -n" in r):
        print(f"no step before the fold writes the store's readings to {path}")
    if "perf-runner-record.sh" in run or "cgroup-of" in run:
        print("the fold waits on the server's counters, which a stopped server cannot give")
    if "always()" not in condition or "steps.stack.outcome == 'success'" not in condition:
        print(f"the fold runs only on a walk that passed ({condition or 'no if'})")
    if judge is None or judge < fold[0]:
        print("the store probe is judged against its marks before its readings are folded in")
upload = [s for s in steps if "upload-artifact" in str(s.get("uses", ""))]
if not upload or str(upload[0].get("with", {}).get("path", "")) != "${{ env.PERF_BUNDLE_DIR }}":
    print("the upload carries something other than the whole bundle directory")
elif "inputs.leg" not in str(upload[0].get("with", {}).get("name", "")):
    print("the upload's name is not the leg's, so two legs of a night overwrite each other")
PY_LEG
)"
if [ -z "$leg_problems" ]; then
  pass "the leg records its runner, reads its verdict and keeps the store's readings whatever became of the server"
else
  fail "the leg:"$'\n'"$leg_problems"
fi

# The cron hour sets a place in the queue order, and the domains go after every other nightly cron,
# each in a slot of its own. Only crons that fire every night count.
daily_crons() {
  sed -n "s/^ *- cron: '\([0-9]*\) \([0-9]*\) \* \* \*'.*/\2 \1/p" "$1"
}
domain_slots=""
for domain in "${DOMAINS[@]}"; do
  slot="$(daily_crons "$domain" | head -1)"
  if [ -n "$slot" ]; then
    domain_slots="$domain_slots$slot"$'\n'
  else
    fail "$(basename "$domain") does not run nightly"
  fi
done
first_hour="$(sort -n <<<"$domain_slots" | awk 'NF { print $1; exit }')"
shared_slots="$(sort <<<"$domain_slots" | uniq -d | awk 'NF')"
if [ "$(grep -c . <<<"$domain_slots")" -eq "${#DOMAINS[@]}" ] && [ -z "$shared_slots" ]; then
  pass "each domain runs nightly in a slot of its own"
else
  fail "the domains share a slot or one runs on no night: $(tr '\n' ',' <<<"$domain_slots")"
fi
behind=""
for other in "$REPO_ROOT"/.github/workflows/*.yml; do
  case " ${DOMAINS[*]} " in *" $other "*) continue ;; esac
  while read -r hour _; do
    [ -n "$hour" ] || continue
    if [ "$hour" -ge "${first_hour:-0}" ]; then
      behind="$behind $(basename "$other"):$hour"
    fi
  done < <(daily_crons "$other")
done
if [ -n "$first_hour" ] && [ -z "$behind" ]; then
  pass "the domains are last in the night's order"
else
  fail "the domains are last in the night's order (not behind:$behind)"
fi

# Each domain runs alone: its own concurrency group, and a trend call that outlives a cancel,
# names the domain, and is handed the run's start and every other job's result.
domain_problems="$(
  python3 - "$TREND_WORKFLOW" "${DOMAINS[@]}" <<'PY_DOMAINS'
import os, re, sys, yaml

trend_file, groups = os.path.basename(sys.argv[1]), {}
for path in sys.argv[2:]:
    name = os.path.basename(path)
    doc = yaml.safe_load(open(path, encoding="utf-8"))
    jobs = doc.get("jobs") or {}
    group = (doc.get("concurrency") or {}).get("group")
    if not group:
        print(f"{name}: no concurrency group, so two of its nights can overlap")
    elif group in groups:
        print(f"{name}: shares the concurrency group {group} with {groups[group]}")
    else:
        groups[group] = name
    calls = [(j, b) for j, b in jobs.items() if str(b.get("uses", "")).endswith(f"/{trend_file}")]
    if len(calls) != 1:
        print(f"{name}: calls the trend {len(calls)} times, where once publishes and alerts the night")
        continue
    job, body = calls[0]
    given = body.get("with") or {}
    if "always()" not in str(body.get("if", "")):
        print(f"{name}: the trend call does not run after a cancel or a timeout, so a night that died is not alerted")
    if given.get("domain") != name.removeprefix("perf-").removesuffix(".yml").replace("catchup", "catch-up"):
        print(f"{name}: the alert names the domain [{given.get('domain')}]")
    if "outputs.started_at" not in str(given.get("started-at", "")):
        print(f"{name}: the trend is not handed the time the run started")
    named = set(re.findall(r"([\w-]+)=\$\{\{ needs", str(given.get("results", ""))))
    others = set(jobs) - {job}
    needs = set(body.get("needs") or [])
    if named != others or needs != others:
        print(f"{name}: the trend reads {sorted(named)} and waits on {sorted(needs)} of the jobs {sorted(others)}")
PY_DOMAINS
)"
if [ -z "$domain_problems" ]; then
  pass "each domain runs alone and hands its trend call the run's start and every job's result"
else
  fail "domains:"$'\n'"$domain_problems"
fi

# Every job that brings the stack up sets production's socket buffers, dials the server by its
# certificate's name, and hands the harness the target's network counters.
path_problems="$(
  python3 - "$LEG_WORKFLOW" "$REPO_ROOT/.github/workflows/soak.yml" <<'PY'
import sys, yaml

jobs_read = 0
for path in sys.argv[1:]:
    with open(path, encoding="utf-8") as fh:
        doc = yaml.safe_load(fh)
    for name, job in doc.get("jobs", {}).items():
        runs = [str(step.get("run", "")) for step in job.get("steps", [])]
        up = [i for i, run in enumerate(runs) if "docker compose" in run and " up " in run]
        if not up:
            continue
        jobs_read += 1
        where = f"{path.rsplit('/', 1)[-1]} job {name}"
        def first(needle):
            return next((i for i, run in enumerate(runs) if needle in run), None)
        raised = first("perf-stack-quic.sh raise-buffers")
        mapped = first("perf-stack-quic.sh map-server")
        walked = first("/tmp/loadtest")
        checked = first("perf-stack-quic.sh check")
        if raised is None or raised > up[0]:
            print(f"{where}: the buffers are not raised before the stack starts")
        if mapped is None or mapped < up[0]:
            print(f"{where}: the server is not mapped to its certificate's name once the stack is up")
        if walked is None:
            print(f"{where}: no harness walk found")
            continue
        walk = runs[walked]
        if "-addr=server:9090" not in walk:
            print(f"{where}: the harness does not dial the server by its certificate's name")
        if '-target-net-counters="$PERF_TARGET_NET_COUNTERS"' not in walk:
            print(f"{where}: the harness is not told where the target's network counters are")
        if checked is None or checked < walked:
            print(f"{where}: nothing fails a run whose ends ran short of receive buffer")
        if "127.0.0.1:9090" in "\n".join(runs):
            print(f"{where}: something still dials the published port on the loopback")
print(f"jobs={jobs_read}")
PY
)"
path_jobs="$(sed -n 's/^jobs=//p' <<<"$path_problems")"
path_problems="$(grep -v '^jobs=' <<<"$path_problems" || true)"
if [ "${path_jobs:-0}" -eq 0 ]; then
  fail "no job bringing the stack up was found, so the path checks read nothing"
elif [ -z "$path_problems" ]; then
  pass "each of $path_jobs jobs gives the machine-facing path production's buffers and no relay"
else
  fail "a job measures the machine-facing path through what production does not have: $path_problems"
fi

# Images are pulled first with bounded retries, and every always-run step after the bring-up
# waits on the stack having come up.
bringup_problems="$(
  python3 - "$LEG_WORKFLOW" "$REPO_ROOT/.github/workflows/soak.yml" <<'PY_BRINGUP'
import sys, yaml

jobs_read = 0
for path in sys.argv[1:]:
    with open(path, encoding="utf-8") as fh:
        doc = yaml.safe_load(fh)
    for name, job in doc.get("jobs", {}).items():
        steps = job.get("steps", [])
        runs = [str(step.get("run", "")) for step in steps]
        up = [i for i, run in enumerate(runs) if "docker compose" in run and " up " in run]
        if not up:
            continue
        jobs_read += 1
        where = f"{path.rsplit('/', 1)[-1]} job {name}"
        pulled = next((i for i, run in enumerate(runs) if "scripts/perf-stack-pull.sh" in run), None)
        if pulled is None or pulled > up[0]:
            print(f"{where}: the images are not pulled with retries before the stack is brought up")
        stack_id = steps[up[0]].get("id")
        if not stack_id:
            print(f"{where}: the bring-up has no id for the steps after it to wait on")
            continue
        for step in steps[up[0] + 1:]:
            condition = str(step.get("if", "") or "")
            run = str(step.get("run", ""))
            if "always()" not in condition or ("docker compose" in run and " down" in run):
                continue
            if f"steps.{stack_id}.outcome" not in condition:
                label = step.get("name", step.get("uses", "?"))
                print(f"{where}: '{label}' runs after a failed bring-up and adds an error of its own")
print(f"jobs={jobs_read}")
PY_BRINGUP
)"
bringup_jobs="$(sed -n 's/^jobs=//p' <<<"$bringup_problems")"
bringup_problems="$(grep -v '^jobs=' <<<"$bringup_problems" || true)"
if [ "${bringup_jobs:-0}" -eq 0 ]; then
  fail "no job bringing the stack up was found, so the bring-up checks read nothing"
elif [ -z "$bringup_problems" ]; then
  pass "each of $bringup_jobs jobs pulls with retries first and reports a failed bring-up once"
else
  fail "a failed bring-up is reported more than once:"$'\n'"$bringup_problems"
fi

# The pull itself: a registry that drops the connection is asked again, a bound
# on it, and a refusal that asking again cannot change is not.
PULL="$REPO_ROOT/scripts/perf-stack-pull.sh"
PULL_WORK="$(mktemp -d)"
mkdir -p "$PULL_WORK/bin"
cat >"$PULL_WORK/bin/docker" <<'STUB'
#!/usr/bin/env bash
n=$(($(cat "$PULL_COUNT" 2>/dev/null || echo 0) + 1))
printf '%s' "$n" >"$PULL_COUNT"
printf '%s\n' "$*" >>"$PULL_ARGS"
if [ "$n" -le "${PULL_FAILS:-0}" ]; then
  printf '%s\n' "$PULL_ERROR" >&2
  exit 1
fi
STUB
chmod +x "$PULL_WORK/bin/docker"
RESET='Error response from daemon: Head "https://registry-1.docker.io/v2/victoriametrics/victoria-metrics/manifests/v1.114.0": Get "https://auth.docker.io/token": read tcp 10.1.1.165:34390->172.64.144.78:443: read: connection reset by peer'
run_pull() { # fails, error
  PULL_STATUS=0
  rm -f "$PULL_WORK/count" "$PULL_WORK/args"
  PULL_OUT="$(PATH="$PULL_WORK/bin:$PATH" PULL_COUNT="$PULL_WORK/count" PULL_ARGS="$PULL_WORK/args" \
    PULL_FAILS="$1" PULL_ERROR="$2" PERF_PULL_DELAY=0 \
    "$PULL" deploy/docker-compose.perf.yml 2>&1)" || PULL_STATUS=$?
  PULL_TRIES="$(cat "$PULL_WORK/count" 2>/dev/null || echo 0)"
}
if [ ! -x "$PULL" ]; then
  fail "scripts/perf-stack-pull.sh is missing or not executable"
else
  run_pull 2 "$RESET"
  if [ "$PULL_STATUS" -eq 0 ] && [ "$PULL_TRIES" = "3" ]; then
    pass "a pull whose connection was reset twice is asked again and succeeds"
  else
    fail "a pull whose connection was reset twice is asked again and succeeds (status=$PULL_STATUS tries=$PULL_TRIES out=[$PULL_OUT])"
  fi
  if grep -qF -- '--ignore-buildable' "$PULL_WORK/args"; then
    pass "it pulls the images and leaves the server to its build"
  else
    fail "it pulls the images and leaves the server to its build (args=[$(cat "$PULL_WORK/args")])"
  fi
  run_pull 99 "$RESET"
  errors="$(grep -c '::error::' <<<"$PULL_OUT" || true)"
  if [ "$PULL_STATUS" -ne 0 ] && [ "$PULL_TRIES" = "4" ] && [ "$errors" = "1" ]; then
    pass "a registry that never answers is given up on after four tries, with one error"
  else
    fail "a registry that never answers is given up on after four tries, with one error (status=$PULL_STATUS tries=$PULL_TRIES errors=$errors)"
  fi
  run_pull 99 'Error response from daemon: manifest for victoriametrics/victoria-metrics:v9 not found: manifest unknown'
  if [ "$PULL_STATUS" -ne 0 ] && [ "$PULL_TRIES" = "1" ]; then
    pass "an image that does not exist is not asked for again"
  else
    fail "an image that does not exist is not asked for again (status=$PULL_STATUS tries=$PULL_TRIES)"
  fi
fi
rm -rf "$PULL_WORK"

# The trend's publish job pushes every leg's rows with the run's start and compares each leg with
# its own nights, and a gate job reads the verdict off the job's result.
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
if "inputs.started-at" not in str(env.get("VM_RUN_STARTED_AT", "")):
    print("the publish job is not handed the time the run started")
if "!cancelled()" not in str(publish.get("if", "")):
    print("the publish job does not run after a leg that failed, or runs on after a cancel")
if not any("oci-kube-setup" in str(s.get("uses", "")) for s in publish.get("steps", [])):
    print("the publish job never reaches the store")
gate = jobs.get("gate") or {}
gate_text = yaml.safe_dump(gate.get("steps", []))
if "publish" not in (gate.get("needs") or []) or "!cancelled()" not in str(gate.get("if", "")):
    print("no gate job reads the publish job whatever it met short of a cancel")
if "needs.publish.result" not in gate_text or "needs.publish.outputs.regression" not in gate_text:
    print("the gate does not read the publish job's result and its finding")
alert = jobs.get("alert") or {}
alert_text = yaml.safe_dump(alert)
if "always()" not in str(alert.get("if", "")) or "inputs.domain" not in alert_text or "inputs.results" not in alert_text:
    print("the alert does not outlive a failed publish, or does not name the domain and its jobs")
PY_PUBLISH
)"
if [ -z "$publish_problems" ]; then
  pass "$(basename "$TREND_WORKFLOW") publishes the legs, gates them against their nights and alerts by domain"
else
  fail "$(basename "$TREND_WORKFLOW"): $publish_problems"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
