#!/usr/bin/env bash
# Holds the workflows to .claude/rules/ci-cd-determinism.md: a CI/CD step whose
# work was refused must not report success.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GUARD="scripts/assert-cache-written.sh"

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   [%s] %s\n' "$1" "$2"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("[$1] $2")
  printf '  FAIL [%s] %s\n' "$1" "$2" >&2
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Reads every workflow once and prints read<TAB>sweep<TAB>N and finding<TAB>sweep<TAB>text.
# The files after the root are the nightly scripts and workflows whose cluster calls it reads.
cat >"$WORK/sweeps.py" <<'PY'
import os
import re
import sys

import yaml

ROOT = sys.argv[1]
RETRY_SCOPE = sys.argv[2:]
GUARD = "scripts/assert-cache-written.sh"
SENDER = "scripts/telegram-alert.sh"
TARGETS = ("x86_64-unknown-linux-musl", "aarch64-unknown-linux-musl")
CACHES = (r"actions/cache", r"Swatinem/rust-cache", r"cache-to:", r"^[ \t]*cache:[ \t]")
STATUS = re.compile(r"\b(always|success|failure|cancelled)\(\)")
ALWAYS = re.compile(r"\balways\(\)")
CLEAN_UP = ("Clean up", "Remove", "Release", "Tear")
TAKE = re.compile(r"scripts/staging-lease\.sh\s+acquire\b")
CHECK = re.compile(r"scripts/staging-version-check\.sh\b")
CAPTURE = re.compile(r"^[ \t]*[A-Za-z_][A-Za-z0-9_]*=\$\?[ \t]*$")
INLINE_CAPTURE = re.compile(r"\|\|[ \t]*[A-Za-z_][A-Za-z0-9_]*=\$\?")
PRINTS = re.compile(r"^(cat|echo|printf|tee|true|:)([ \t]|$)")
CLUSTER_CALL = re.compile(r"kubectl[^|&;]*(exec|cp)[ \t]")


def report(sweep, text):
    print(f"finding\t{sweep}\t{text}")


def reached(sweep, count):
    print(f"read\t{sweep}\t{count}")


def read(rel):
    path = os.path.join(ROOT, rel)
    return open(path, encoding="utf-8").read() if os.path.isfile(path) else None


NAMES = sorted(f for f in os.listdir(os.path.join(ROOT, ".github/workflows")) if f.endswith(".yml"))
TEXT = {name: read(f".github/workflows/{name}") for name in NAMES}
DOCS = {name: yaml.safe_load(TEXT[name]) or {} for name in NAMES}


def jobs_of(name):
    return {job: body for job, body in (DOCS[name].get("jobs") or {}).items() if isinstance(body, dict)}


def steps_of(job):
    return [step for step in job.get("steps") or [] if isinstance(step, dict)]


def needs_of(job):
    needs = job.get("needs") or []
    return [needs] if isinstance(needs, str) else list(needs)


def caches():
    text = TEXT.get("cd.yml")
    for shape in CACHES if text is not None else ():
        if re.search(shape, text, re.M):
            report("cache-undeclared", f"cd.yml declares a cache ({shape})")
    reached("cache-undeclared", 1 if text is not None else 0)
    saves = [name for name in NAMES if "actions/cache/save@" in TEXT[name]]
    for name in saves:
        if GUARD not in TEXT[name]:
            report("cache-read-back", f"{name} writes a cache entry and never reads it back")
    image = TEXT.get("build-image.yml") or ""
    for needed in (GUARD,) + TARGETS:
        if needed not in image:
            report("cache-read-back", f"build-image.yml never reads back the agent build's cache for {needed}")
    reached("cache-read-back", len(saves) + (1 if image else 0))


def mutation_reports():
    uploads = 0
    for job, body in jobs_of("mutation.yml").items() if "mutation.yml" in DOCS else ():
        for step in steps_of(body):
            if not str(step.get("uses", "")).startswith("actions/upload-artifact@"):
                continue
            uploads += 1
            if (step.get("with") or {}).get("if-no-files-found") != "error":
                report("mutation-report", f"mutation.yml {job}: '{step.get('name')}' passes an empty file set")
    reached("mutation-report", uploads)


def status_reads():
    blocks = 0
    for name in NAMES:
        for job, body in jobs_of(name).items():
            for step in steps_of(body):
                run = step.get("run")
                if not isinstance(run, str):
                    continue
                where = f"{name} {job}: '{step.get('name') or step.get('id') or run.splitlines()[0]}'"
                guarded, captures, prev = False, 0, ""
                for line in run.split("\n"):
                    if not line.strip() or line.lstrip().startswith("#"):
                        continue
                    guarded = guarded or bool(re.search(r"set[ \t]+\+e", line))
                    if INLINE_CAPTURE.search(line):
                        prev = line.strip()
                        continue
                    if CAPTURE.match(line):
                        captures += 1
                        if not guarded:
                            report("errexit-status", f"{where} reads $? under errexit")
                        if PRINTS.match(prev):
                            report("errexit-status", f"{where} reads the status of a print")
                    prev = line.strip()
                blocks += 1 if captures else 0
    reached("errexit-status", blocks)


def environments():
    named = 0
    for name in NAMES:
        for job, body in jobs_of(name).items():
            if "environment" not in body:
                continue
            named += 1
            environment = body["environment"]
            if isinstance(environment, dict):
                environment = environment.get("name", "")
            if environment not in ("staging", "production"):
                report("approved-environment", f"{name}:{job} names {environment}")
    reached("approved-environment", named)


def staging_slot():
    takers = 0
    for name in NAMES:
        for job, body in jobs_of(name).items() if name != "cd.yml" else ():
            steps = steps_of(body)
            takes = [i for i, s in enumerate(steps) if TAKE.search(str(s.get("run", "")))]
            if not takes:
                continue
            takers += 1
            where = f"{name}:{job}"
            after = steps[takes[0] + 1] if takes[0] + 1 < len(steps) else {}
            if not CHECK.search(str(after.get("run", ""))):
                report("staging-build", f"{where}: the step after taking the slot does not check staging's build")
            elif not str((after.get("env") or {}).get("UNDER_TEST", "")).strip():
                report("staging-build", f"{where}: the check names no commit under test")
            elif not after.get("id"):
                report("staging-build", f"{where}: the check has no id, so its reason cannot reach the alert")
            elif f"steps.{after['id']}.outputs.not_started" not in str(body.get("outputs") or {}):
                report("staging-build", f"{where}: the job does not hand the check's reason to its outputs")
            checkouts = [s for s in steps if str(s.get("uses", "")).startswith("actions/checkout@")]
            if not any(str((c.get("with") or {}).get("fetch-depth", "")) == "0" for c in checkouts):
                report("staging-build", f"{where}: its checkout is shallow, so staging's commit cannot be read")
    reached("staging-build", takers)


def ancestors(jobs, job, seen=None):
    seen = set() if seen is None else seen
    for parent in needs_of(jobs.get(job, {})):
        if parent not in seen:
            seen.add(parent)
            ancestors(jobs, parent, seen)
    return seen


def job_status():
    below = outliving = 0
    for name in NAMES:
        jobs = jobs_of(name)
        for job, body in jobs.items():
            condition = str(body.get("if", "") or "")
            skippable = sorted(a for a in ancestors(jobs, job) if "if" in jobs.get(a, {}))
            if skippable:
                below += 1
                if not STATUS.search(condition):
                    report("stated-status", f"{name}:{job} reads success() of {', '.join(skippable)}, which can be skipped")
            if not ALWAYS.search(condition):
                continue
            outliving += 1
            sends = any(SENDER in str(s.get("run", "")) for s in steps_of(body))
            if not sends and not str(body.get("name", "")).startswith(CLEAN_UP):
                report("outlives-cancel", f"{name}:{job} runs on after a cancel and neither alerts nor cleans up")
    reached("stated-status", below)
    reached("outlives-cancel", outliving)


def cluster_calls():
    calls = 0
    for rel in RETRY_SCOPE:
        text = read(rel)
        if text is None:
            report("cluster-retry", f"{rel} is in the retry sweep's scope and is not there")
            continue
        lines, comments, index = text.split("\n"), "", 0
        while index < len(lines):
            line, start = lines[index], index + 1
            index += 1
            if not line.strip():
                comments = ""
                continue
            if line.lstrip().startswith("#"):
                comments += line + " "
                continue
            while re.search(r"\\[ \t]*$", line) and index < len(lines):
                line = re.sub(r"\\[ \t]*$", "", line) + " " + lines[index]
                index += 1
            if not CLUSTER_CALL.search(line):
                continue
            calls += 1
            block = comments + line
            comments = ""
            if "kubectl_retry" not in block and "Not retried" not in block:
                report("cluster-retry", f"{rel}:{start} is a bare cluster call with no reason beside it")
    reached("cluster-retry", calls)


caches()
mutation_reports()
status_reads()
environments()
staging_slot()
job_status()
cluster_calls()
PY

read_of() { awk -F'\t' -v s="$2" '$1 == "read" && $2 == s { print $3 }' <<<"$1"; }
findings_of() { awk -F'\t' -v s="$2" '$1 == "finding" && $2 == s { print "      " $3 }' <<<"$1"; }

# Judges one sweep: the demonstration finds exactly what was planted, the repository nothing.
judge() { # id, expected demo findings, what the sweep holds
  local id="$1" expected="$2" holds="$3" demo_found real_found count
  demo_found="$(findings_of "$DEMO_OUT" "$id" | sed 's/^ *//' | sort)"
  if [ "$demo_found" = "$(sort <<<"$expected")" ]; then
    pass "$id" "demonstrated: the sweep finds exactly the defects planted for it"
  else
    fail "$id" "demonstrated: expected [$expected], found [$demo_found]"
  fi
  count="$(read_of "$REAL_OUT" "$id")"
  real_found="$(findings_of "$REAL_OUT" "$id")"
  if [ "${count:-0}" -eq 0 ]; then
    fail "$id" "the sweep reached nothing, so it is asserting an absence it never tested"
  elif [ -z "$real_found" ]; then
    pass "$id" "${holds/N/$count}"
  else
    fail "$id" "${holds/N/$count}, except:"$'\n'"$real_found"
  fi
}

echo "ci-cd-determinism:"

DEMO="$WORK/demo"
mkdir -p "$DEMO/.github/workflows" "$DEMO/scripts"
cat >"$DEMO/.github/workflows/cd.yml" <<'YAML'
on: workflow_dispatch
jobs:
  resolve:
    if: github.event_name == 'workflow_dispatch'
    steps:
      - run: echo resolve
  cluster:
    needs: resolve
    if: needs.resolve.outputs.changed == 'true'
    steps:
      - run: echo cluster
  deploy:
    needs: [resolve, cluster]
    if: always() && needs.resolve.result == 'success'
    environment: staging
    steps:
      - uses: actions/cache@v4
  drills:
    needs: [resolve, deploy]
    if: vars.DRILLS == 'true'
    steps:
      - run: echo drill
  promote:
    needs: [deploy, drills]
    if: ${{ !cancelled() && needs.drills.result == 'success' }}
    environment:
      name: production
    steps:
      - run: echo promote
  alert:
    needs: [deploy]
    if: always()
    steps:
      - run: scripts/telegram-alert.sh "x"
  tidy:
    name: Remove the staging machines
    needs: [deploy]
    if: always()
    steps:
      - run: echo tidy
  observe:
    environment: observability
    steps:
      - run: echo observe
YAML
cat >"$DEMO/.github/workflows/ci.yml" <<'YAML'
on: push
jobs:
  build:
    steps:
      - uses: actions/cache/save@v4
      - name: Unguarded
        run: |
          some-check
          rc=$?
      - name: Guarded
        run: |
          set +e
          some-check
          rc=$?
          set -e
      - name: Printed
        run: |
          set +e
          some-check
          echo done
          rc=$?
      - name: Inline
        run: some-check || rc=$?
YAML
cat >"$DEMO/.github/workflows/build-image.yml" <<'YAML'
on: push
jobs:
  agent:
    steps:
      - run: cargo build --target x86_64-unknown-linux-musl
YAML
cat >"$DEMO/.github/workflows/mutation.yml" <<'YAML'
on: schedule
jobs:
  shard:
    steps:
      - name: Upload report
        uses: actions/upload-artifact@v7
        with:
          if-no-files-found: error
      - name: Upload status
        uses: actions/upload-artifact@v7
YAML
cat >"$DEMO/.github/workflows/drill.yml" <<'YAML'
on: schedule
jobs:
  unguarded:
    steps:
      - uses: actions/checkout@v7
      - run: scripts/staging-lease.sh acquire "$HOLDER"
      - run: scripts/fault/pod-delete.sh
  guarded:
    outputs:
      not_started: ${{ steps.version.outputs.not_started }}
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
      - run: scripts/staging-lease.sh acquire "$HOLDER"
      - id: version
        env:
          UNDER_TEST: ${{ github.sha }}
        run: scripts/staging-version-check.sh >>"$GITHUB_OUTPUT"
YAML
cat >"$DEMO/scripts/nightly.sh" <<'SH'
kubectl exec pod -- true
# Not retried: a second run repeats the fault.
kubectl -n ns exec pod -- false

kubectl_retry exec pod -- true
kubectl cp \
  pod:/tmp/out out
SH

DEMO_OUT="$(python3 "$WORK/sweeps.py" "$DEMO" scripts/nightly.sh scripts/absent.sh)"
REAL_OUT="$(python3 "$WORK/sweeps.py" "$REPO_ROOT" \
  .github/workflows/network-drill.yml \
  .github/workflows/load-test.yml \
  scripts/loadtest-k6-incluster.sh \
  scripts/loadtest-quic-incluster.sh \
  scripts/loadtest-cleanup.sh \
  deploy/scripts/pg-app-role-sql.sh \
  deploy/scripts/loadtest-account-sql.sh)"

if [ -x "$REPO_ROOT/$GUARD" ]; then
  pass cache-read-back "the read-back guard is executable"
else
  fail cache-read-back "the read-back guard is missing or not executable"
fi
judge cache-read-back "ci.yml writes a cache entry and never reads it back
build-image.yml never reads back the agent build's cache for $GUARD
build-image.yml never reads back the agent build's cache for aarch64-unknown-linux-musl" \
  "each of N workflows writing a cache entry reads it back, the agent build's for both targets"

judge cache-undeclared "cd.yml declares a cache (actions/cache)" \
  "cd.yml, whose token cannot write, declares no cache of any kind"

judge mutation-report "mutation.yml shard: 'Upload status' passes an empty file set" \
  "all N mutation.yml artifact uploads fail on an empty file set"

judge staging-build "drill.yml:unguarded: the step after taking the slot does not check staging's build
drill.yml:unguarded: its checkout is shallow, so staging's commit cannot be read" \
  "each of N jobs taking the staging slot checks staging runs the build it tests"

# GitHub runs `run:` blocks under `bash -e`, so a failing command ends the step before `rc=$?`.
demo_captured=$(
  bash -e -c '
    reached=no
    false
    rc=$?
    reached=yes
    echo "rc=$rc reached=$reached"
  ' 2>/dev/null || true
)
demo_guarded=$(
  bash -e -c '
    set +e
    false
    rc=$?
    set -e
    echo "rc=$rc reached=yes"
  ' 2>/dev/null || true
)
demo_after_print=$(bash -c 'set +e; false; cat /dev/null >&2; rc=$?; echo "$rc"')
demo_before_print=$(bash -c 'set +e; false; rc=$?; cat /dev/null >&2; echo "$rc"')
if [ -z "$demo_captured" ] && [ "$demo_guarded" = "rc=1 reached=yes" ] \
  && [ "$demo_after_print" = 0 ] && [ "$demo_before_print" = 1 ]; then
  pass errexit-status "a status read under errexit is unreachable, and one read after a print holds the print's"
else
  fail errexit-status "the errexit demonstration no longer reproduces (unguarded='$demo_captured' guarded='$demo_guarded' after='$demo_after_print' before='$demo_before_print')"
fi
judge errexit-status "ci.yml build: 'Unguarded' reads \$? under errexit
ci.yml build: 'Printed' reads the status of a print" \
  "each of N workflow steps reading an exit status turns errexit off first and reads its command's"

judge approved-environment "cd.yml:observe names observability" \
  "every one of N environments a workflow names is one a person approves"

judge cluster-retry "scripts/nightly.sh:1 is a bare cluster call with no reason beside it
scripts/nightly.sh:6 is a bare cluster call with no reason beside it
scripts/absent.sh is in the retry sweep's scope and is not there" \
  "each of N cluster calls in the nightlies is retried or says why it is not"

judge stated-status "cd.yml:cluster reads success() of resolve, which can be skipped
cd.yml:drills reads success() of cluster, deploy, resolve, which can be skipped" \
  "each of N jobs below a job that can be skipped states the status it runs under"

judge outlives-cancel "cd.yml:deploy runs on after a cancel and neither alerts nor cleans up" \
  "each of N jobs that run on after a cancel sends an alert or cleans up"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
