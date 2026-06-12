#!/usr/bin/env bash
# Tests for centralized, authenticated Docker Hub fallback configuration.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ACTION="$REPO_ROOT/.github/actions/docker-hub-mirror/action.yml"
WORKFLOWS="$REPO_ROOT/.github/workflows"

PASS=0
FAIL=0
FAILURES=()

pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }

input_is_optional() {
  local input_name="$1"

  awk -v input_name="$input_name" '
    $0 == "  " input_name ":" {
      in_input = 1
      next
    }
    in_input && /^  [a-zA-Z0-9_-]+:/ {
      exit
    }
    in_input && /^[[:space:]]+required:[[:space:]]+false$/ {
      optional = 1
    }
    in_input && /^[[:space:]]+default:[[:space:]]+""$/ {
      empty_default = 1
    }
    END {
      exit !(in_input && optional && empty_default)
    }
  ' "$ACTION"
}

echo "docker-hub-mirror:"

if [ ! -f "$ACTION" ]; then
  fail "composite action exists"
else
  pass "composite action exists"
fi

mapfile -t MIRROR_MATCHES < <(grep -rnF 'registry-mirrors' "$REPO_ROOT/.github" || true)
if [ "${#MIRROR_MATCHES[@]}" -eq 1 ] &&
  [[ "${MIRROR_MATCHES[0]}" == "$ACTION:"* ]]; then
  pass "registry-mirrors has one canonical definition"
else
  fail "registry-mirrors must appear exactly once under .github, only in the composite"
fi

mapfile -t PULL_JOBS < <(
  for workflow in "$WORKFLOWS"/*.yml; do
    awk '
      function flush_job() {
        if (job_name != "" && first_pull_line > 0) {
          printf "%s:%s:%d:%d\n", FILENAME, job_name, mirror_line, first_pull_line
        }
      }

      function is_pull_line(line) {
        return line ~ /docker[[:space:]]+pull[[:space:]]/ || line ~ /docker[[:space:]]+run[[:space:]]/ || line ~ /docker[[:space:]]+compose.*[[:space:]]up([[:space:]]|$)/ || line ~ /^      image:[[:space:]]/ || line ~ /^        image:[[:space:]]/
      }

      /^jobs:[[:space:]]*$/ {
        in_jobs = 1
        next
      }

      in_jobs && /^[^[:space:]#]/ {
        flush_job()
        in_jobs = 0
        job_name = ""
      }

      in_jobs && /^  [a-zA-Z0-9_-]+:[[:space:]]*$/ {
        flush_job()
        job_name = $1
        sub(/:$/, "", job_name)
        mirror_line = 0
        first_pull_line = 0
        next
      }

      !in_jobs || job_name == "" || /^[[:space:]]*#/ {
        next
      }

      index($0, "uses: ./.github/actions/docker-hub-mirror") {
        mirror_line = FNR
      }

      first_pull_line == 0 && is_pull_line($0) {
        first_pull_line = FNR
      }

      END {
        flush_job()
      }
    ' "$workflow"
  done
)

if [ "${#PULL_JOBS[@]}" -eq 6 ]; then
  pass "expected six Docker Hub pull-capable jobs"
else
  fail "expected six Docker Hub pull-capable jobs, found ${#PULL_JOBS[@]}"
fi

UNPROTECTED_JOBS=()
for pull_job in "${PULL_JOBS[@]}"; do
  IFS=: read -r workflow job_name mirror_line pull_line <<<"$pull_job"
  if [ "$mirror_line" -eq 0 ] || [ "$mirror_line" -ge "$pull_line" ]; then
    UNPROTECTED_JOBS+=("${workflow#"$REPO_ROOT/"}:$job_name")
  fi
done

if [ "${#UNPROTECTED_JOBS[@]}" -eq 0 ]; then
  pass "every pull-capable job configures the mirror first"
else
  fail "pull-capable jobs missing an earlier mirror step: ${UNPROTECTED_JOBS[*]}"
fi

read -r COMPOSITE_USES BAD_CREDENTIAL_BLOCKS < <(
  awk '
    function flush_use() {
      if (!in_use) {
        return
      }
      use_count++
      if (!has_username || !has_token) {
        bad_count++
      }
      in_use = 0
      has_username = 0
      has_token = 0
    }

    FNR == 1 {
      flush_use()
    }

    /^      - uses: \.\/\.github\/actions\/docker-hub-mirror[[:space:]]*$/ {
      flush_use()
      in_use = 1
      next
    }

    in_use && /^      - / {
      flush_use()
    }

    in_use && index($0, "dockerhub-username: ${{ secrets.DOCKERHUB_USERNAME }}") {
      has_username = 1
    }

    in_use && index($0, "dockerhub-token: ${{ secrets.DOCKERHUB_TOKEN }}") {
      has_token = 1
    }

    END {
      flush_use()
      printf "%d %d\n", use_count, bad_count
    }
  ' "$WORKFLOWS"/*.yml
)

if [ "$COMPOSITE_USES" -eq 6 ]; then
  pass "all six pull jobs use the composite"
else
  fail "expected six composite uses, found $COMPOSITE_USES"
fi

if [ "$BAD_CREDENTIAL_BLOCKS" -eq 0 ]; then
  pass "every composite use passes optional Docker Hub credentials"
else
  fail "$BAD_CREDENTIAL_BLOCKS composite use(s) omit Docker Hub credentials"
fi

if input_is_optional "dockerhub-username" && input_is_optional "dockerhub-token"; then
  pass "Docker Hub credential inputs are optional"
else
  fail "Docker Hub credential inputs must be optional with empty defaults"
fi

if grep -qF "if: inputs.dockerhub-token != ''" "$ACTION"; then
  pass "Docker Hub login is gated on token presence"
else
  fail "Docker Hub login must be gated on token presence"
fi

if grep -qF "echo \"\$DH_TOKEN\" | docker login -u \"\$DH_USER\" --password-stdin" "$ACTION"; then
  pass "Docker Hub token uses password-stdin"
else
  fail "Docker Hub login must pass the token through password-stdin"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
