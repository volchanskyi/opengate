#!/usr/bin/env bash
# The runner-hosted performance stack must be able to host the load it is for.
#
# The disposable stack this replaces could not: it published only the
# browser-facing port, so no simulated machine could reach it; it declared no
# resource bounds, so "the server ran out of processor" and "the database did"
# were the same observation; and it ran no metrics store, so a slow run could
# report latency and nothing about why.
#
# Run: ./scripts/tests/perf-stack.test.sh
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

# A machine reaches the server over QUIC, which is UDP. A stack that publishes
# only the browser port can host operator load and no fleet at all.
if grep -qE '^\s*-\s*"9090:9090/udp"' "$COMPOSE"; then
  pass "the machine-facing QUIC port is published"
else
  fail "the machine-facing QUIC port is published"
fi

# Every service is bounded, separately. Without that, the sweep has nothing to
# vary and a saturated stack cannot say which part saturated.
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

# The server's processor count is the sweep's variable, so it cannot be a
# literal: several stacks that also differ in unrecorded ways are not a sweep.
if grep -q 'PERF_SERVER_CPUS' "$COMPOSE"; then
  pass "the server's processor count is the sweep's variable"
else
  fail "the server's processor count is the sweep's variable"
fi

# The volume family measures how much disk a fixture occupies. A database in
# memory has no size, so this stack must not put its data on tmpfs.
postgres_block() {
  awk '
    /^  postgres:/ { in_svc = 1; next }
    /^  [a-z-]+:$/ { in_svc = 0 }
    in_svc { print }
  ' "$COMPOSE"
}
# Read once into a variable: piped into `grep -q` the reader stops at its
# first match, and pipefail reports the writer's failed write as a service that
# names neither thing.
POSTGRES_BLOCK="$(postgres_block)"
if grep -qE '^[[:space:]]+tmpfs:' <<<"$POSTGRES_BLOCK"; then
  fail "the database writes to tmpfs, so a fixture would have no measurable size"
elif grep -qF 'perf-postgres:/var/lib/postgresql/data' <<<"$POSTGRES_BLOCK"; then
  pass "the database writes to a volume, so a fixture has a measurable size"
else
  fail "the database names no data volume"
fi

# A stack that cannot observe itself reports latency and nothing about why.
if grep -q 'victoria-metrics' "$COMPOSE"; then
  pass "the stack runs a metrics store"
else
  fail "the stack runs a metrics store"
fi

# A variable no Go source reads is a setting somebody will one day try to
# change, expecting something to happen.
if grep -q 'OPENGATE_TEST_MODE' "$COMPOSE"; then
  fail "the perf stack sets OPENGATE_TEST_MODE, which no Go source reads"
else
  pass "the perf stack sets no variable the server does not read"
fi

# Compose must be able to parse it — a stack nobody can bring up is not a stack.
if command -v docker >/dev/null 2>&1; then
  if DOCKER_CONFIG="$("$REPO_ROOT/scripts/docker-credstore-guard.sh")" \
    docker compose -f "$COMPOSE" config >/dev/null 2>&1; then
    pass "compose parses the perf stack"
  else
    fail "compose parses the perf stack"
  fi
else
  # A machine without Docker still checks the shape above; only the parse is
  # skipped, and it is stated rather than counted as a pass.
  echo "  note docker not on PATH; compose parse not exercised here (CI runs it)"
fi

# --- A default that fires on an empty value is not a default on an unset one ---
#
# The endurance family sets PERF_SERVER_GO_LDFLAGS to the empty string, and that
# empty string is the whole point: an empty link keeps the target's symbol table,
# and a core dump is addresses until something can read them back as types.
#
# Compose's `${VAR:-default}` substitutes its default for a variable that is set
# and empty as well as for one that is unset, so the value the workflow chose was
# discarded and the release link came back. The soak of 2026-09-13 walked its five
# hours, reached the reference walk and refused at the first thing it checks —
# "carries no debugging information" — on the first night that walk had ever run.
# `${VAR-default}` is the form that means what the workflow meant.
#
# The contract is stated in the workflow and satisfied in the compose file, so it
# is checked against both: every variable a workflow deliberately empties is read
# off the workflows, and the form the compose file reads it with is read off the
# compose file.
emptied=()
while IFS= read -r name; do
  [ -n "$name" ] && emptied+=("$name")
done < <(
  grep -rhoE "^[[:space:]]+[A-Z][A-Z0-9_]*:[[:space:]]*''[[:space:]]*$" "$REPO_ROOT"/.github/workflows/*.yml \
    | sed -E "s/^[[:space:]]+([A-Z0-9_]+):.*/\1/" | sort -u
)

# A sweep that reached nothing passes for the wrong reason. The soak's own
# variable is the one it must always find.
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

# And the punctuation is a proxy for what compose does with it, so compose is
# asked. The variable is set exactly the way the soak sets it, and what comes
# back has to be the empty link rather than the release one.
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

# The workflow must drive the two families this stack exists for, and must not
# claim absolute capacity from a runner.
# The volume family is a sweep over machines enrolled rather than a single run,
# so it is matched by prefix: the fixture names do not differ in how much data
# they hold, and the count that does is in the profile's own name.
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

# Every path a step names must exist from where that step runs. A step that
# changes directory into server/ and then names a repository-root script does
# not fail on a missing flag or a bad argument — it fails with "No such file or
# directory" after the stack is already up, which is how all four legs of the
# sweep died on the one night this workflow has ever run.
missing_paths="$(
  python3 - "$WORKFLOW" "$REPO_ROOT" <<'PY'
import os
import re
import sys

import yaml

workflow, root = sys.argv[1], sys.argv[2]
with open(workflow, encoding="utf-8") as handle:
    document = yaml.safe_load(handle)

# Anything that looks like a repository path the step hands to a program.
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

# The weighing counts rows in tables the schema actually has. Numeric readings
# live in the metrics store, not in Postgres, so a count of a readings table is
# a query that can only ever fail.
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

# --- Every family enrols; none signs its own ----------------------------------
#
# The stack's certificate authority is created by the server, inside a container
# whose /data is a tmpfs. A harness given no enrolment URL builds an authority of
# its own in a temp directory instead, and the two are then different generations
# of the same-named authority: every dial is refused with "certificate signed by
# unknown authority ... OpenGate CA". All four scaling shards died that way on
# every run the workflow has ever had, 3300 refusals apiece.
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

# The scaling family holds the data constant and varies the processors, so the
# fixture its profile declares has to actually be built. Running it against an
# empty database holds the data constant at nothing.
if grep -q -- '-fixture-account=' <<<"$(job_block scaling)"; then
  pass "the scaling family builds the fleet its profile declares"
else
  fail "the scaling family measures against an empty database, so its data is not the profile's"
fi

# --- A leg is the unit, because a family's legs do not all offer the same load -
#
# The volume family varies how much data is already there. Reading is what more
# data slows, and the only reader this venue had was machines arriving — the
# cheapest thing the server does, and one whose cost barely moves with the size
# of the fleet already in the database. So the sweep varied its variable against
# a load that could not feel it, which is the same shape that left the scaling
# curve flat from one processor upwards.
#
# Held at family level these checks answer the wrong question for a family whose
# legs differ. The shape family's three legs are one job, and `journeys` varies
# across them: two open the sessions their profiles declare and the third does
# not. A family-level check reads the one generator invocation in the block,
# finds it, and reports the whole family covered — crediting the leg that offers
# nothing with the load its neighbours offer. So the unit here is the leg.
#
# legs_of <family> — one record per leg: its profile, then whether that leg
# offers a browser-side generator.
#
# A family whose legs vary the profile spells them in `matrix.include`; one that
# varies something else names its profile once in the run block. A leg that
# declares `journeys:` has said which it is, and where none does, every leg of a
# family that starts a generator offers one.
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

# folds_gate <block> — the condition on the step that folds the browser-side
# numbers in, or empty where nothing gates it.
folds_gate() {
  awk '
    /^      - name:/ { cond = "" }
    /^        if:/ { c = $0; sub(/^[[:space:]]*if:[[:space:]]*/, "", c); cond = c }
    /--journeys/ { print cond; exit }
  ' <<<"$1"
}

# A leg that declares a technician load and deliberately offers none, with the
# reason it is that way. An exemption is re-earned: the sweep fails on an entry
# whose leg now offers a generator, so one cannot outlive what it was for.
declare -A OFFERS_NO_JOURNEYS=(
  # This leg raises the fleet until the server gives out — sixteen thousand
  # machines on a shared runner, with a hundred and sixty held sessions on top.
  # What that generator would need of the runner is a reading to take before the
  # leg offers it, and the harness reports its own headroom on every leg now, so
  # the reading is takeable.
  ["shapes load/profiles/breakpoint.yaml"]="the ladder's generator allowance is a reading still to be taken"
)

# Every leg names a profile this gate can read, and there is at least one, so a
# sweep that stopped enumerating legs fails rather than passing over an empty
# list.
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

# And what a browser-side generator times is written into another process's
# file. A leg that starts one and never folds it in has produced readings in a
# directory the job destroys; one that folds without starting one folds an
# export that was never written. Both directions, per leg, because a family
# whose legs differ is satisfied at family level by doing each once.
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

# And the two halves of a leg walk the same shape. The generator reads its walk
# from LOADTEST_PROFILE and the harness from -profile, and nothing about either
# name says they are the same decision — so a leg can offer fifteen journeys a
# second against a fleet arriving to a different profile's ramp, and every
# number it produces is about a night nobody scheduled.
#
# A family whose legs vary the profile spells it through the matrix, so what is
# compared is the expression rather than a resolved path: the check is that one
# decision reaches both halves.
for family in volume scaling shapes; do
  block="$(job_block "$family")"
  grep -q 'loadtest-k6-alongside\.sh' <<<"$block" || continue

  generator_walks="$(sed -n 's/^[[:space:]]*LOADTEST_PROFILE:[[:space:]]*//p' <<<"$block" | sed -n 1p)"
  # The value is taken whole rather than to the first space: a matrix expression
  # carries spaces of its own, and cutting at one compares half a name.
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

# --- A capacity claim is made about the load the product actually costs -------
#
# Both families answer "how many machines can one processor hold", which is the
# kind of finding a reader acts on. They answered it for a fleet that is
# connected and idle: nobody was watching a screen. A technician holding a remote
# session is the expensive thing the product does, so a ceiling read without one
# is a ceiling for a load that never happens.
#
# Every profile on both families declares `sessions`, and a declared number
# nothing offers is an intention rather than a fact — the same gap the endurance
# family closed, one venue over. The scenarios are read off the invocation rather
# than listed here, so a scenario added to or taken off a leg is judged by what
# it offers.
#
# A scenario's executor says which of the two technician numbers it offers:
# arrivals are a rate of journeys, sessions are a count held open, and neither
# stands in for the other.
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

  # The sessions a leg declares are held open beside its fleet, and the harness
  # answers the machine end of them. A leg that offers no generator is judged by
  # the exemption above rather than twice here.
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

  # The scenarios a family runs and the exports it folds are one invocation and
  # one list, shared by every leg that reaches them, so they are read once. Both
  # directions: a leg that starts a generator and folds nothing has produced
  # readings in a directory the job destroys, and a fold naming an export
  # nothing runs fails on the night rather than here.
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

# --- The bundle's verdict is read back ----------------------------------------
#
# The harness writes what it thought of its own run into the bundle, and the
# volume family passed a run whose bundle said "invalid" and whose fleet was
# 0/500. A verdict nothing reads is a string in a file, which is the same defect
# as a cache save nobody asserts.
if grep -q 'verdict' "$WORKFLOW"; then
  pass "the workflow reads the verdict the harness wrote about the run"
else
  fail "the workflow never reads the bundle's verdict, so a run that measured nothing passes"
fi

# Weekly answered a question that changes when the schema or the read paths do,
# so it runs nightly. The hour it names is not a time it starts — every
# scheduled run begins four and a half to six and a half hours later — so what
# the hour buys is a place in the order, and this one goes last: the whole
# night's batch is in front of it, and a run scheduled beside them queues behind
# them on a twenty-job pool rather than running.
#
# Last is therefore the property to hold, rather than the number seven.
# Only the crons that fire every night are the night's batch. A weekly run is
# in a different queue on a different day, and holding this one behind it would
# be a claim about a Sunday.
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

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
