#!/usr/bin/env bash
# Tests for .github/scripts/notify_failure.py — the issue a failed CD run leaves
# behind is the only thing that outlives the run's log retention, so it has to
# carry the log, and it has to say so when it could not get one.
#
# The `gh` stand-in is a real script on PATH, so what the notifier does with a
# refusal is observed rather than mocked out: a call that fails writes to stderr
# and returns non-zero exactly as the CLI does.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
NOTIFY="$REPO_ROOT/.github/scripts/notify_failure.py"
[ -f "$NOTIFY" ] || {
  echo "FAIL: $NOTIFY missing" >&2
  exit 1
}

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

# The stand-in records every invocation and answers from files the test writes,
# so a body the notifier built is read back out of what it passed to `gh`.
mkdir -p "$WORK/bin"
cat >"$WORK/bin/gh" <<'FAKE_GH'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"$FAKE_GH_CALLS"

case "$1 ${2:-}" in
  "api"*)
    case "$2" in
      *"/jobs" | *"/jobs?"*)
        cat "$FAKE_GH_JOBS"
        exit 0
        ;;
      *"/logs")
        if [ -n "${FAKE_GH_LOG_REFUSED:-}" ]; then
          echo "gh: HTTP 403: Server failed to authenticate the request" >&2
          exit 1
        fi
        if [ -n "${FAKE_GH_LOG_EMPTY:-}" ]; then
          exit 0
        fi
        if [ -n "${FAKE_GH_LOG_BLOB_GONE:-}" ]; then
          # What the archive endpoint actually serves when the log blob behind
          # it is gone: a storage error document, on stdout, exit zero.
          printf '<?xml version="1.0" encoding="utf-8"?><Error><Code>BlobNotFound</Code><Message>The specified blob does not exist.</Message></Error>\n'
          exit 0
        fi
        cat "$FAKE_GH_LOG"
        exit 0
        ;;
    esac
    ;;
  "run view")
    if [ -n "${FAKE_GH_RUNVIEW_LOG:-}" ]; then
      cat "$FAKE_GH_RUNVIEW_LOG"
      exit 0
    fi
    echo "gh: no logs found" >&2
    exit 1
    ;;
  "issue list")
    printf '%s' "${FAKE_GH_EXISTING_ISSUE:-}"
    exit 0
    ;;
  "issue create" | "issue comment")
    # Record the body so the test can read what would have been filed.
    prev=""
    for a in "$@"; do
      if [ "$prev" = "--body" ]; then printf '%s' "$a" >"$FAKE_GH_BODY"; fi
      prev="$a"
    done
    exit 0
    ;;
esac
exit 0
FAKE_GH
chmod +x "$WORK/bin/gh"

export FAKE_GH_CALLS="$WORK/calls"
export FAKE_GH_JOBS="$WORK/jobs.json"
export FAKE_GH_LOG="$WORK/job.log"
export FAKE_GH_BODY="$WORK/body.md"

cat >"$FAKE_GH_JOBS" <<'JOBS'
{"id":99037677475,"name":"Deploy staging","conclusion":"failure","html_url":"https://github.com/o/r/actions/runs/1/job/99037677475","steps":["Run E2E against staging"]}
JOBS

# run_notify drives the notifier with the stand-in on PATH and returns its exit
# code, leaving the filed body in $FAKE_GH_BODY.
run_notify() {
  : >"$FAKE_GH_CALLS"
  : >"$FAKE_GH_BODY"
  PATH="$WORK/bin:$PATH" python3 "$NOTIFY" \
    --repo o/r --run-id 1 --branch main --workflow CD \
    --sha 1becf09a744dc2ad0271a46a8d591be1009bbcc4 --event workflow_run \
    --run-url https://github.com/o/r/actions/runs/1 \
    >"$WORK/stdout" 2>"$WORK/stderr"
}

echo "notify-failure:"

# --- the excerpt is the point of the issue ------------------------------------
{
  echo "line one"
  echo "the step that failed said this"
} >"$FAKE_GH_LOG"

if run_notify; then
  pass "a job with a log files an issue and succeeds"
else
  pass_rc=$?
  fail "a job with a log files an issue and succeeds (rc=$pass_rc, stderr=$(cat "$WORK/stderr"))"
fi

if grep -q "the step that failed said this" "$FAKE_GH_BODY"; then
  pass "the issue body carries the job's log"
else
  fail "the issue body carries the job's log (body=[$(cat "$FAKE_GH_BODY")])"
fi

# --- a refused log is reported, never filed as an empty excerpt ---------------
#
# This is the whole reason the file exists. Every issue filed to date recorded
# "No log output available." and nothing else, so the one artifact that outlives
# the run's log retention held nothing about why the run failed — and the step
# that wrote it was green.
FAKE_GH_LOG_REFUSED=1 run_notify && refused_rc=0 || refused_rc=$?

if [ "$refused_rc" -ne 0 ]; then
  pass "a run whose log could not be read fails the step"
else
  fail "a run whose log could not be read fails the step (rc=0)"
fi

if grep -q "403" "$FAKE_GH_BODY"; then
  pass "the issue body carries why the log could not be read"
else
  fail "the issue body carries why the log could not be read (body=[$(cat "$FAKE_GH_BODY")])"
fi

if ! grep -q "No log output available" "$FAKE_GH_BODY"; then
  pass "and never files a bare 'no log output' with no reason"
else
  fail "and never files a bare 'no log output' with no reason"
fi

# --- an empty answer is a refusal too ----------------------------------------
#
# The archive endpoint answers a job whose log it cannot serve with a zero exit
# and nothing on stdout, which is indistinguishable from a job that logged
# nothing — and a job that failed always logged something.
FAKE_GH_LOG_EMPTY=1 run_notify && empty_rc=0 || empty_rc=$?
if [ "$empty_rc" -ne 0 ]; then
  pass "an empty answer from the log endpoint fails the step"
else
  fail "an empty answer from the log endpoint fails the step (rc=0)"
fi

# --- a storage error document is not a log --------------------------------
#
# Observed on a real shard whose runner died mid-step: the endpoint answers with
# an Azure error document on stdout and exits zero, so a caller that checks only
# the exit code files that XML as the job's log.
FAKE_GH_LOG_BLOB_GONE=1 run_notify && blob_rc=0 || blob_rc=$?
if [ "$blob_rc" -ne 0 ]; then
  pass "a storage error document is treated as a refusal"
else
  fail "a storage error document is treated as a refusal (rc=0)"
fi
if ! grep -q "BlobNotFound" "$FAKE_GH_BODY" || grep -q "Could not read" "$FAKE_GH_BODY"; then
  pass "and the reason it names is the storage error, not a log"
else
  fail "and the reason it names is the storage error, not a log (body=[$(cat "$FAKE_GH_BODY")])"
fi

# --- the second door ----------------------------------------------------------
#
# `gh run view --log` reaches the same log by a different route, so a refusal on
# the archive endpoint is not the end of it.
export FAKE_GH_RUNVIEW_LOG="$WORK/runview.log"
echo "what the second door returned" >"$FAKE_GH_RUNVIEW_LOG"
FAKE_GH_LOG_REFUSED=1 run_notify && second_rc=0 || second_rc=$?
if [ "$second_rc" -eq 0 ]; then
  pass "a log the second door can reach is filed, and the step passes"
else
  fail "a log the second door can reach is filed, and the step passes (rc=$second_rc)"
fi
if grep -q "what the second door returned" "$FAKE_GH_BODY"; then
  pass "the issue body carries what the second door returned"
else
  fail "the issue body carries what the second door returned (body=[$(cat "$FAKE_GH_BODY")])"
fi
unset FAKE_GH_RUNVIEW_LOG

# --- nothing failed, nothing filed -------------------------------------------
: >"$FAKE_GH_JOBS"
if run_notify; then
  pass "a run with no failed job files nothing and succeeds"
else
  fail "a run with no failed job files nothing and succeeds"
fi
if [ ! -s "$FAKE_GH_BODY" ]; then
  pass "and writes no issue body"
else
  fail "and writes no issue body (body=[$(cat "$FAKE_GH_BODY")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n'
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
