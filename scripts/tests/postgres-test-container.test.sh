#!/usr/bin/env bash
# Every start of the test Postgres declares a 1g /dev/shm and the same connection settings.
# Parallel query workers place segments in /dev/shm, which Docker's 64 MiB default cannot hold.
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

# Files are found by the image they start, so a new caller is swept when it is written.
sources="$(grep -rl 'postgres:17-alpine' \
  "$REPO_ROOT/Makefile" "$REPO_ROOT/.github/workflows" 2>/dev/null || true)"

if [ -z "$sources" ]; then
  fail "no file starts postgres:17-alpine any more — this sweep reached nothing"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

# starts_in joins each `docker run` of the test image onto one line so wrapped commands read whole.
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

    # The declared shared memory holds more than one parallel worker's segment.
    if ! grep -qE -- '--shm-size[= ]1g' <<<"$start"; then
      missing_shm="$missing_shm [$rel]"
    fi

    # The connection settings match in every caller, so none raises the ceiling alone.
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

# Reading the image's default /dev/shm shows the flag guards a real limit; this needs Docker.
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
