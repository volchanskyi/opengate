#!/usr/bin/env bash
# The test Postgres is started the same way everywhere, and its shared memory
# fits what it is configured to use.
#
# Six places start it — the Makefile target the gauntlet's prerequisite phase
# calls, and five workflow steps — and every one of them raises
# max_connections to 400 so the parallel per-schema suite does not saturate the
# 100-connection default. None of them said anything about shared memory, so all
# six took Docker's 64 MiB default for /dev/shm.
#
# That is not enough for what those settings ask for. Postgres puts a parallel
# query's workers in shared memory, and one of them asked for 32 MiB: two at
# once exhaust the 64. What it looks like from outside is not a memory error in
# a test but the database going away mid-run — `could not resize shared memory
# segment to 33554432 bytes: No space left on device`, then `connection
# refused` from everything after it, on a host with 189 GB free. The container
# runs with `--rm`, so it removes itself on the way out and leaves nothing to
# inspect.
#
# It is load-dependent, so it passes far more often than it fails, which is the
# property that makes a sweep over the text worth more than a run: the settings
# are checkable without reproducing the conditions.
#
# Run: ./scripts/tests/postgres-test-container.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

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

echo "postgres test container:"

# Every file that starts the test Postgres, found by the image it starts rather
# than by a list — a new caller is swept the day it is written.
sources="$(grep -rl 'postgres:17-alpine' \
  "$REPO_ROOT/Makefile" "$REPO_ROOT/.github/workflows" 2>/dev/null || true)"

if [ -z "$sources" ]; then
  fail "no file starts postgres:17-alpine any more — this sweep reached nothing"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

# starts_in FILE — each `docker run` of the test image, joined onto one line so
# a command written across continuations is read whole. A sweep that reads one
# line at a time sees none of them, because every one of these is wrapped.
starts_in() {
  sed -e ':a' -e '/\\$/{N;s/\\\n//;ta' -e '}' "$1" \
    | grep -E 'docker run .*postgres:17-alpine' || true
}

checked=0
missing_shm=""
mismatched=""
while IFS= read -r file; do
  [ -n "$file" ] || continue
  rel="${file#"$REPO_ROOT/"}"
  while IFS= read -r start; do
    [ -n "$start" ] || continue
    checked=$((checked + 1))

    # Shared memory is declared, and declared large enough to hold more than one
    # parallel worker's segment at a time.
    if ! grep -qE -- '--shm-size[= ]1g' <<<"$start"; then
      missing_shm="$missing_shm [$rel]"
    fi

    # And the settings that make it necessary are the same everywhere, so a
    # caller cannot raise the connection ceiling in one place alone.
    grep -qE -- '-c max_connections=400' <<<"$start" \
      || mismatched="$mismatched [$rel:max_connections]"
    grep -qE -- '-c max_locks_per_transaction=256' <<<"$start" \
      || mismatched="$mismatched [$rel:max_locks_per_transaction]"
  done < <(starts_in "$file")
done <<<"$sources"

if [ "$checked" -ge 5 ]; then
  pass "reached $checked start(s) of the test database"
else
  fail "reached only $checked start(s) — the way it is started changed shape"
fi

if [ -z "$missing_shm" ]; then
  pass "every start declares shared memory its settings can actually use"
else
  fail "a test database is started on Docker's 64 MiB default /dev/shm, which one parallel worker can exhaust:$missing_shm"
fi

if [ -z "$mismatched" ]; then
  pass "every start configures the same connection and lock ceilings"
else
  fail "the test database is configured differently in different places:$mismatched"
fi

# --- The default really is too small ------------------------------------------
#
# The sweep above is a statement about text. This is the reading behind it, so a
# guard that has stopped policing a real limit fails rather than going on
# checking a flag for its own sake. It needs Docker; without it the reading is
# stated as unavailable rather than counted as a pass.
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  default_shm="$(docker run --rm --entrypoint sh postgres:17-alpine \
    -c 'df -k /dev/shm | tail -1 | awk "{print \$2}"' 2>/dev/null || true)"
  if [ -n "$default_shm" ] && [ "$default_shm" -lt 131072 ]; then
    pass "the image's default /dev/shm is ${default_shm} KiB, below the 128 MiB two workers need"
  elif [ -n "$default_shm" ]; then
    fail "the image's default /dev/shm is now ${default_shm} KiB — re-measure what a parallel worker asks for before trusting this guard"
  else
    fail "could not read the image's default /dev/shm, so the reading behind this guard was not taken"
  fi
else
  echo "  --   docker is not available, so the default /dev/shm was not read"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
