#!/usr/bin/env bash
# DOCKER_BIN is a stub that records what the scanner would have read from the snapshot it was
# handed, then answers as SonarCloud would for STUB_MODE. Every case runs in a throwaway repo.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCAN="$SCRIPT_DIR/../sonar-scan.sh"
[ -x "$SCAN" ] || {
  echo "FAIL: $SCAN not found or not executable" >&2
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
expect_eq() {
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want [$3] got [$2])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK:?}"' EXIT

STUB="$WORK/docker"
cat >"$STUB" <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
wd=""
prev=""
user=""
for arg in "$@"; do
  case "$prev" in
    -w) wd="$arg" ;;
    --user) user="$arg" ;;
  esac
  prev="$arg"
done
calls=$(($(cat "$STUB_STATE/calls" 2>/dev/null || echo 0) + 1))
echo "$calls" >"$STUB_STATE/calls"
{
  echo "user=$user"
  echo "parent=$(git -C "$wd" rev-parse HEAD^)"
  echo "edited=$(git -C "$wd" show HEAD:tracked.txt)"
  echo "untracked=$(git -C "$wd" show HEAD:new.txt)"
  if git -C "$wd" cat-file -e HEAD:ignored.txt 2>/dev/null; then echo "ignored=in"; else echo "ignored=out"; fi
  if [ -f "$wd/server/coverage.out" ]; then echo "coverage=carried"; else echo "coverage=missing"; fi
} >"$STUB_STATE/seen"
case "$STUB_MODE" in
  pass)
    mkdir -p "$wd/.scannerwork/scanner-report"
    echo fresh >"$wd/.scannerwork/scanner-report/marker"
    echo "ANALYSIS SUCCESSFUL"
    ;;
  gate)
    mkdir -p "$wd/.scannerwork/scanner-report"
    echo fresh >"$wd/.scannerwork/scanner-report/marker"
    echo "ERROR QUALITY GATE STATUS: FAILED"
    exit 3
    ;;
  flaky)
    if [ "$calls" -lt 2 ]; then
      echo "java.io.EOFException"
      exit 1
    fi
    mkdir -p "$wd/.scannerwork/scanner-report"
    echo fresh >"$wd/.scannerwork/scanner-report/marker"
    ;;
  down)
    echo "java.io.EOFException"
    exit 1
    ;;
esac
EOF
chmod +x "$STUB"

# Builds a repo holding a committed edit, an untracked file, an ignored file and a coverage report.
make_repo() {
  REPO="$WORK/repo-$1"
  STUB_STATE="$WORK/state-$1"
  mkdir -p "$REPO" "$STUB_STATE"
  git -C "$REPO" init --quiet --initial-branch=dev
  git -C "$REPO" config user.name "Ivan Volchanskyi"
  git -C "$REPO" config user.email "ivan.volchanskyi@gmail.com"
  printf 'ignored.txt\nserver/coverage.out\n.scannerwork/\n' >"$REPO/.gitignore"
  echo 'sonar.go.coverage.reportPaths=server/coverage.out' >"$REPO/sonar-project.properties"
  echo committed >"$REPO/tracked.txt"
  git -C "$REPO" add -A
  git -C "$REPO" commit --quiet -m init
  echo uncommitted >"$REPO/tracked.txt"
  echo brand-new >"$REPO/new.txt"
  echo secret >"$REPO/ignored.txt"
  mkdir -p "$REPO/server" "$REPO/.scannerwork/scanner-report"
  echo 'mode: set' >"$REPO/server/coverage.out"
  echo stale >"$REPO/.scannerwork/scanner-report/marker"
}

run_scan() {
  (cd "$REPO" && SONAR_TOKEN=t DOCKER_BIN="$STUB" STUB_MODE="$1" STUB_STATE="$STUB_STATE" \
    SONAR_SCAN_RETRY_SLEEP=0 "$SCAN" >"$STUB_STATE/out" 2>&1)
}

seen() { sed -n "s/^$1=//p" "$STUB_STATE/seen"; }

echo "a passing scan:"
make_repo pass
head_before="$(git -C "$REPO" rev-parse HEAD)"
status_before="$(git -C "$REPO" status --porcelain)"
rc=0
run_scan pass || rc=$?
expect_eq "exits 0" "$rc" 0
expect_eq "the snapshot sits on HEAD" "$(seen parent)" "$head_before"
expect_eq "the snapshot carries the uncommitted edit" "$(seen edited)" uncommitted
expect_eq "the snapshot carries the untracked file" "$(seen untracked)" brand-new
expect_eq "the snapshot leaves the ignored file out" "$(seen ignored)" out
expect_eq "the coverage report travels with the snapshot" "$(seen coverage)" carried
expect_eq "the scanner runs as the caller" "$(seen user)" "$(id -u):$(id -g)"
expect_eq "the kept report comes back to the work tree" \
  "$(cat "$REPO/.scannerwork/scanner-report/marker" 2>/dev/null)" fresh
expect_eq "HEAD stays where it was" "$(git -C "$REPO" rev-parse HEAD)" "$head_before"
expect_eq "the index and work tree are untouched" "$(git -C "$REPO" status --porcelain)" "$status_before"
expect_eq "the snapshot worktree is gone" "$(git -C "$REPO" worktree list | wc -l | tr -d ' ')" 1

echo
echo "a failed quality gate:"
make_repo gate
rc=0
run_scan gate || rc=$?
expect_eq "exits 1" "$rc" 1
expect_eq "is not retried" "$(cat "$STUB_STATE/calls")" 1
expect_eq "still brings the report back for the guards that follow" \
  "$(cat "$REPO/.scannerwork/scanner-report/marker" 2>/dev/null)" fresh
expect_eq "the snapshot worktree is gone" "$(git -C "$REPO" worktree list | wc -l | tr -d ' ')" 1

echo
echo "a transient failure:"
make_repo flaky
rc=0
run_scan flaky || rc=$?
expect_eq "is retried to a pass" "$rc" 0
expect_eq "took two attempts" "$(cat "$STUB_STATE/calls")" 2

echo
echo "every attempt failing:"
make_repo down
rc=0
run_scan down || rc=$?
expect_eq "exits 1" "$rc" 1
expect_eq "tries three times" "$(cat "$STUB_STATE/calls")" 3
if [ -e "$REPO/.scannerwork/scanner-report/marker" ]; then
  fail "leaves no report from an earlier scan for the guards to read"
else pass "leaves no report from an earlier scan for the guards to read"; fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
