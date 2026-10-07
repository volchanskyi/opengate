#!/usr/bin/env bash
# The runner-hosted performance stack must be able to host the load it is for.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE="$REPO_ROOT/deploy/docker-compose.perf.yml"
WORKFLOW="$REPO_ROOT/.github/workflows/perf-stack.yml"
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

for f in "$COMPOSE" "$WORKFLOW"; do
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

for family in volume- scaling; do
  if grep -q "load/profiles/${family}" "$WORKFLOW"; then
    pass "the workflow runs the ${family%-} profile"
  else
    fail "the workflow runs the ${family%-} profile"
  fi
done

if grep -q 'weigh' "$WORKFLOW"; then
  pass "the workflow weighs the fixture it built"
else
  fail "the workflow weighs the fixture it built"
fi

missing_paths="$(
  python3 - "$WORKFLOW" "$REPO_ROOT" <<'PY'
import os
import re
import sys

import yaml

workflow, root = sys.argv[1], sys.argv[2]
with open(workflow, encoding="utf-8") as handle:
    document = yaml.safe_load(handle)

candidate = re.compile(r"(?<![\w/.-])((?:scripts|load|deploy|policy|benchmarks)/[\w./-]+)")
missing = []
for job in document.get("jobs", {}).values():
    job_dir = (job.get("defaults", {}).get("run", {}) or {}).get("working-directory", "")
    for step in job.get("steps", []) or []:
        script = step.get("run")
        if not script:
            continue
        step_dir = step.get("working-directory", job_dir) or ""
        for path in set(candidate.findall(script)):
            if path.endswith((".", "/")):
                continue
            resolved = os.path.normpath(os.path.join(root, step_dir, path))
            if not os.path.exists(resolved):
                missing.append(f"{step.get('name', '?')}: {path} (from {step_dir or '.'})")
for row in sorted(missing):
    print(row)
PY
)"
if [ -z "$missing_paths" ]; then
  pass "every path a step names exists from where that step runs"
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

# Each family passes -enroll-url so the server signs the certificates the harness uses.
job_block() {
  awk -v job="$1" '
    $0 ~ "^  " job ":$" { in_job = 1; next }
    /^  [a-z-]+:$/ { in_job = 0 }
    in_job { print }
  ' "$WORKFLOW"
}
for family in volume scaling shapes; do
  block="$(job_block "$family")"
  if grep -q -- '-enroll-url=' <<<"$block"; then
    pass "the $family family asks the server to sign, so it holds the server's own authority"
  else
    fail "the $family family signs its own certificates against an authority the server never made"
  fi
done

if grep -q -- '-fixture-account=' <<<"$(job_block scaling)"; then
  pass "the scaling family builds the fleet its profile declares"
else
  fail "the scaling family measures against an empty database, so its data is not the profile's"
fi

# legs_of <family> prints one record per leg: its profile, then whether that leg offers a
# browser-side generator.
legs_of() {
  local family="$1" block default_offers include profile
  block="$(job_block "$family")"
  default_offers=false
  grep -q 'loadtest-k6-alongside\.sh' <<<"$block" && default_offers=true

  include="$(awk '
    /^        include:[[:space:]]*$/ { inc = 1; next }
    inc && /^        [^ ]/ { inc = 0 }
    inc { print }
  ' <<<"$block")"

  if [ -z "$include" ]; then
    profile="$(grep -oE 'load/profiles/[a-z0-9.-]+\.yaml' <<<"$block" | sort -u)"
    printf '%s\t%s\n' "$profile" "$default_offers"
    return 0
  fi

  awk -v fallback="$default_offers" '
    function flush() {
      if (seen) printf "%s\t%s\n", path, (journeys == "" ? fallback : journeys)
    }
    /^          - / {
      flush()
      seen = 1; path = ""; journeys = ""
      line = $0
      sub(/^          - /, "", line)
      $0 = "            " line
    }
    /^            path:[[:space:]]/ {
      v = $0; sub(/^[[:space:]]*path:[[:space:]]*/, "", v); path = v; next
    }
    /^            journeys:[[:space:]]/ {
      v = $0; sub(/^[[:space:]]*journeys:[[:space:]]*/, "", v); journeys = v; next
    }
    END { flush() }
  ' <<<"$include"
}

# folds_gate <block> prints the condition on the step that folds the browser-side numbers in.
folds_gate() {
  awk '
    /^      - name:/ { cond = "" }
    /^        if:/ { c = $0; sub(/^[[:space:]]*if:[[:space:]]*/, "", c); cond = c }
    /--journeys/ { print cond; exit }
  ' <<<"$1"
}

# Legs that declare a technician load and deliberately offer none; an entry fails once its leg
# offers a generator.
declare -A OFFERS_NO_JOURNEYS=(
  # The breakpoint leg raises the fleet until the server gives out, and offers no generator yet.
  ["shapes load/profiles/breakpoint.yaml"]="the generator's room at a rung that held is a reading still to be taken"
)

# Every leg names a readable profile, and at least one leg is read.
legs_read=0
for family in volume scaling shapes; do
  while IFS=$'\t' read -r profile journeys; do
    if [ -z "$profile" ]; then
      fail "the $family family has a leg that names no profile, so every check below holds it to nothing"
      continue
    fi
    if [ ! -f "$REPO_ROOT/$profile" ]; then
      fail "the $family family names $profile and there is no such profile"
      continue
    fi
    legs_read=$((legs_read + 1))
  done < <(legs_of "$family")
done
if [ "$legs_read" -gt 0 ]; then
  pass "the three families present $legs_read leg(s) this gate can read"
else
  fail "no leg was enumerated at all, so every check below is asserting an absence it never tested"
fi

# A leg that declares a technician load offers one. Where it deliberately does
# not, the exemption is written down and re-earned here.
exempt_unused=""
for family in volume scaling shapes; do
  while IFS=$'\t' read -r profile journeys; do
    [ -n "$profile" ] && [ -f "$REPO_ROOT/$profile" ] || continue
    phases="$(profile_phases "$REPO_ROOT/$profile" 2>/dev/null)" || continue
    declares="$(jq '[.[] | select(.sessions > 0 or .arrivals_per_second > 0)] | length' <<<"$phases")"
    key="$family $profile"
    exempt="${OFFERS_NO_JOURNEYS[$key]:-}"

    if [ "$declares" -eq 0 ]; then
      fail "the $family family's $profile declares no technician load at all, so its capacity claim is about an idle fleet by declaration"
    elif [ "$journeys" = true ]; then
      if [ -n "$exempt" ]; then
        exempt_unused="$exempt_unused"$'\n'"      $key"
      else
        pass "$family's $(basename "$profile" .yaml) leg offers the technician load its profile declares"
      fi
    elif [ -n "$exempt" ]; then
      pass "$family's $(basename "$profile" .yaml) leg offers none, for a reason written down: $exempt"
    else
      fail "the $family family's $profile declares a technician load in $declares phase(s) and its leg offers none, so it varies its variable against machines arriving and nothing else"
    fi
  done < <(legs_of "$family")
done
if [ -z "$exempt_unused" ]; then
  pass "every written-down exemption still describes a leg that offers nothing"
else
  fail "a leg is exempted from offering a technician load and now offers one, so the exemption has outlived its reason:$exempt_unused"
fi

# A leg that starts a generator folds its export in, and a leg that folds one starts a generator.
for family in volume scaling shapes; do
  block="$(job_block "$family")"
  gate="$(folds_gate "$block")"
  folds_at_all=no
  grep -q -- '--journeys' <<<"$block" && folds_at_all=yes

  while IFS=$'\t' read -r profile journeys; do
    [ -n "$profile" ] || continue
    runs="$journeys"
    folds=false
    if [ "$folds_at_all" = yes ]; then
      case "$gate" in
        '') folds=true ;;
        *matrix.journeys*) folds="$journeys" ;;
        *) folds=true ;;
      esac
    fi
    if [ "$runs" = "$folds" ]; then
      pass "$family's $(basename "$profile" .yaml) leg offers and folds its technician numbers together"
    else
      fail "$family's $(basename "$profile" .yaml) leg runs a browser-side generator ($runs) and folds its journeys ($folds), which do not agree"
    fi
  done < <(legs_of "$family")
done

# The generator's LOADTEST_PROFILE and the harness's -profile name the same profile expression.
for family in volume scaling shapes; do
  block="$(job_block "$family")"
  grep -q 'loadtest-k6-alongside\.sh' <<<"$block" || continue

  generator_walks="$(sed -n 's/^[[:space:]]*LOADTEST_PROFILE:[[:space:]]*//p' <<<"$block" | sed -n 1p)"
  # The value is taken whole, because a matrix expression carries spaces of its own.
  harness_walks="$(sed -n 's/^[[:space:]]*-profile=//p' <<<"$block" | sed -n 1p)"
  harness_walks="${harness_walks%%[[:space:]]\\}"
  harness_walks="${harness_walks#\"}"
  harness_walks="${harness_walks%\"}"

  if [ -z "$generator_walks" ]; then
    fail "the $family family starts a browser-side generator and names no profile for it, so the generator is refused where it stands and the leg offers no technician load at all"
  elif [ "$generator_walks" = "$harness_walks" ]; then
    pass "the $family family's generator and harness walk the same profile ($generator_walks)"
  else
    fail "the $family family's generator walks $generator_walks and its harness walks $harness_walks, so the two halves of the leg offer different shapes"
  fi
done

# Every declared `sessions` count is offered by a scenario read off the invocation.
# An executor offers either arrivals (a journey rate) or sessions (a count held open).
scenarios_on() {
  local block="$1" invocation word
  invocation="$(grep -oE 'loadtest-k6-alongside\.sh[^&]*' <<<"$block" | head -1 || true)"
  local -a words=()
  read -r -a words <<<"$invocation"
  # Everything after the script and the harness's output path is a scenario.
  for word in "${words[@]:2}"; do
    case "$word" in
      -* | '' | '&' | \\) continue ;;
      *) printf '%s\n' "$word" ;;
    esac
  done
}

for family in volume scaling shapes; do
  block="$(job_block "$family")"

  # The sessions a leg declares are held open beside its fleet, and the harness answers them.
  while IFS=$'\t' read -r profile journeys; do
    [ -n "$profile" ] && [ -f "$REPO_ROOT/$profile" ] || continue
    [ "$journeys" = true ] || continue
    leg="$(basename "$profile" .yaml)"

    phases="$(profile_phases "$REPO_ROOT/$profile" 2>/dev/null)" || continue
    declared_sessions="$(jq '[.[] | select(.sessions > 0)] | length' <<<"$phases")"
    [ "$declared_sessions" -gt 0 ] || continue

    offers_sessions=no
    while IFS= read -r scenario; do
      [ -n "$scenario" ] || continue
      script="$REPO_ROOT/load/k6/scenarios/$scenario.js"
      if [ ! -f "$script" ]; then
        fail "the $family family names scenario $scenario and there is no such script"
        continue
      fi
      grep -q 'sessionScenarios' "$script" && offers_sessions=yes
    done < <(scenarios_on "$block")

    if [ "$offers_sessions" = yes ]; then
      pass "the sessions $family's $leg leg declares are held open beside its fleet"
    else
      fail "$family's $leg leg declares sessions in $declared_sessions phase(s) and no scenario on it holds one open, so its capacity claim is read with nobody watching a screen"
    fi

    if grep -q -- '-relay-sessions' <<<"$block"; then
      pass "$family's $leg leg has a harness that joins the machine side of a session and echoes"
    else
      fail "$family's $leg leg is not told to answer a session request, so the browser side times a frame nobody sends back"
    fi
  done < <(legs_of "$family")

  # The scenarios a family runs and the exports it folds are one invocation and one shared list.
  grep -q 'loadtest-k6-alongside\.sh' <<<"$block" || continue

  folded=0
  while IFS= read -r scenario; do
    [ -n "$scenario" ] || continue
    if grep -q -- "--journeys[^|]*$scenario\.json" <<<"$block"; then
      folded=$((folded + 1))
    else
      fail "the $family family offers $scenario and folds its numbers nowhere"
    fi
  done < <(scenarios_on "$block")
  if [ "$folded" -gt 0 ]; then
    pass "the $family family folds $folded scenario(s) it runs into its own evidence"
  else
    fail "the $family family folds no scenario it runs, so this sweep checked nothing"
  fi

  while IFS= read -r export_name; do
    [ -n "$export_name" ] || continue
    if grep -qxF "$export_name" <<<"$(scenarios_on "$block")"; then
      pass "the export the $family family folds for $export_name is one its leg produces"
    else
      fail "the $family family folds $export_name's export and its leg never runs it"
    fi
  done < <(grep -oE '\-\-journeys [^ ]+' <<<"$block" \
    | sed -nE 's|.*/([a-z0-9-]+)\.json.*|\1|p' | sort -u)
done

if grep -q 'verdict' "$WORKFLOW"; then
  pass "the workflow reads the verdict the harness wrote about the run"
else
  fail "the workflow never reads the bundle's verdict, so a run that measured nothing passes"
fi

# The cron hour sets a place in the queue order, and this run goes after every nightly cron.
# Only crons that fire every night count; a weekly cron queues on another day.
daily_hours() {
  sed -n "s/^ *- cron: '[0-9]* \([0-9]*\) \* \* \*'.*/\1/p" "$1"
}

perf_hour="$(daily_hours "$WORKFLOW" | head -1)"
if [ -n "$perf_hour" ]; then
  pass "the stack runs nightly"
else
  fail "the stack runs nightly"
fi

behind=""
for other in "$REPO_ROOT"/.github/workflows/*.yml; do
  [ "$other" = "$WORKFLOW" ] && continue
  while read -r hour; do
    [ -n "$hour" ] || continue
    if [ "$hour" -ge "${perf_hour:-0}" ]; then
      behind="$behind $(basename "$other"):$hour"
    fi
  done < <(daily_hours "$other")
done
if [ -z "$behind" ]; then
  pass "the stack is last in the night's order"
else
  fail "the stack is last in the night's order (not behind:$behind)"
fi

# Every job that brings the stack up sets production's socket buffers, dials the server by its
# certificate's name, and hands the harness the target's network counters.
path_problems="$(
  python3 - "$WORKFLOW" "$REPO_ROOT/.github/workflows/soak.yml" <<'PY'
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
  python3 - "$WORKFLOW" "$REPO_ROOT/.github/workflows/soak.yml" <<'PY_BRINGUP'
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

TREND_WORKFLOW="$WORKFLOW"
# A publish job pushes every leg's rows with the run's start and compares each leg with its own
# nights, and a gate job reads the verdict off the job's result.
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

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
