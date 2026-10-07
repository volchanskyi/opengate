#!/usr/bin/env bash
# Scans a snapshot commit of the work tree from a throwaway worktree. SonarCloud raises issues only
# on committed content, so uncommitted edits and new files are committed there before the scan.
#
# Usage: scripts/sonar-scan.sh
#
# Environment:
#   SONAR_TOKEN             (required) the token the scan uses
#   SONAR_SCAN_ATTEMPTS     attempts against a transient failure, default 3
#   SONAR_SCAN_RETRY_SLEEP  seconds per attempt number between attempts, default 15
#   DOCKER_BIN              docker binary, default docker (stubbed in tests)
#
# Exit codes:
#   0  the analysis passed the quality gate
#   1  the gate failed, or every attempt failed
set -euo pipefail

: "${SONAR_TOKEN:?SONAR_TOKEN is required}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tool-versions.sh
. "$SCRIPT_DIR/lib/tool-versions.sh"
DOCKER_BIN="${DOCKER_BIN:-docker}"
attempts="${SONAR_SCAN_ATTEMPTS:-3}"
retry_sleep="${SONAR_SCAN_RETRY_SLEEP:-15}"

root="$(git rev-parse --show-toplevel)"
cd "$root"
common="$(cd "$(git rev-parse --git-common-dir)" && pwd)"
scratch="$(mktemp -d)"
snapshot="$scratch/tree"

cleanup() {
  git worktree remove --force "$snapshot" >/dev/null 2>&1 || true
  rm -rf "${scratch:?}"
  git worktree prune
}
trap cleanup EXIT

# A report from an earlier scan never stands in for this one.
rm -rf "${root:?}/.scannerwork"

# The throwaway index starts from the real one, so staged entries stay,
# and then takes every unignored file.
index="$scratch/index"
real_index="$(git rev-parse --git-path index)"
if [ -f "$real_index" ]; then cp "$real_index" "$index"; fi
GIT_INDEX_FILE="$index" git add -A
tree="$(GIT_INDEX_FILE="$index" git write-tree)"
commit="$(git commit-tree "$tree" -p HEAD -m "sonar snapshot of the work tree")"
git worktree add --quiet --detach "$snapshot" "$commit"

# The coverage reports are ignored build output, so they are carried beside the snapshot's tree.
while IFS= read -r report; do
  [ -n "$report" ] && [ -f "$report" ] || continue
  mkdir -p "$snapshot/$(dirname "$report")"
  cp "$report" "$snapshot/$report"
done < <(sed -n 's/^sonar\.[a-z.]*\.reportPaths=//p' sonar-project.properties | tr ',' '\n')

# The worktree and the repository's git directory are mounted at their own paths, which the
# worktree's link names. The caller's user owns both.
scan() {
  "$DOCKER_BIN" run --rm \
    --user "$(id -u):$(id -g)" \
    -e SONAR_TOKEN="$SONAR_TOKEN" \
    -e SONAR_USER_HOME=/tmp/.sonar \
    -v "$snapshot:$snapshot" -v "$common:$common" -w "$snapshot" \
    "sonarsource/sonar-scanner-cli:$TOOL_VERSION_SONAR_SCANNER_IMAGE" \
    -Dsonar.qualitygate.wait=true \
    -Dsonar.scanner.skipJreProvisioning=true \
    -Dsonar.scanner.keepReport=true \
    -Dsonar.working.directory=.scannerwork \
    -Dsonar.branch.name=dev 2>&1
}

# The kept report comes back to the work tree, where the guards after the scan read it.
collect() {
  if [ -d "$snapshot/.scannerwork" ]; then cp -R "$snapshot/.scannerwork" "$root/.scannerwork"; fi
}

for attempt in $(seq 1 "$attempts"); do
  rc=0
  out="$(scan)" || rc=$?
  printf '%s\n' "$out"
  if [ "$rc" -eq 0 ]; then
    collect
    exit 0
  fi
  if grep -qF 'QUALITY GATE STATUS: FAILED' <<<"$out"; then
    collect
    echo "::error::sonar quality gate FAILED — fix coverage/issues (not transient, not retrying)"
    exit 1
  fi
  if [ "$attempt" -ge "$attempts" ]; then
    echo "::error::sonar scan failed after $attempts attempts (transient infra error)"
    exit 1
  fi
  echo "::warning::sonar scan attempt $attempt failed (transient, such as a plugin-CDN EOFException); retrying in $((attempt * retry_sleep))s"
  sleep $((attempt * retry_sleep))
done
