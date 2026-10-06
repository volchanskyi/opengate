#!/usr/bin/env bash
# Tests for .github/scripts/notify_failure.py, with a `gh` stand-in script on PATH.
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
        # What gh does with a log carrying terminal colour: it refuses to hand
        # it over unless asked to. Every cargo and go job sets a colour term, so
        # this is the ordinary case rather than an edge one.
        if [ -n "${FAKE_GH_LOG_NEEDS_ESCAPE_FLAG:-}" ]; then
          case "$*" in
            *--allow-escape-sequences*) ;;
            *)
              echo "gh: the response contains terminal escape sequences; pass --allow-escape-sequences to output it anyway" >&2
              exit 1
              ;;
          esac
        fi
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

FAKE_GH_LOG_NEEDS_ESCAPE_FLAG=1 run_notify && coloured_rc=0 || coloured_rc=$?
if [ "$coloured_rc" -eq 0 ]; then
  pass "a log carrying terminal colour is fetched rather than refused"
else
  fail "a log carrying terminal colour must still be fetched (rc=$coloured_rc)"
fi
if grep -q "the step that failed said this" "$FAKE_GH_BODY"; then
  pass "the coloured log's content reaches the issue"
else
  fail "the coloured log's content must reach the issue"
fi

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

FAKE_GH_LOG_EMPTY=1 run_notify && empty_rc=0 || empty_rc=$?
if [ "$empty_rc" -ne 0 ]; then
  pass "an empty answer from the log endpoint fails the step"
else
  fail "an empty answer from the log endpoint fails the step (rc=0)"
fi

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
