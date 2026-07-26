#!/usr/bin/env bash
# Tests for scripts/test-go.sh — the shared-container provisioner behind
# `make test-go`.
#
# testpg/testvm memoize per test BINARY, and `go test ./...` builds one binary
# per package, so unset URLs make every Postgres-touching package start its own
# container. The provisioner starts one of each and exports the URLs; these
# tests pin that contract with a mocked `docker`/`curl` on PATH — no real
# container is ever started.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROVISION="$ROOT/scripts/test-go.sh"
[ -x "$PROVISION" ] || {
  echo "FAIL: $PROVISION not executable" >&2
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
assert_eq() {
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}
assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  if printf '%s' "$haystack" | grep -qF -- "$needle"; then pass "$name"; else
    fail "$name (missing [$needle] in: $haystack)"
  fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# --- mocked container runtime -------------------------------------------------
# `docker` records its argv and succeeds; `curl` always reports the VM healthy so
# the readiness poll returns on the first attempt.
BIN_DIR="$WORK/bin"
mkdir -p "$BIN_DIR"
cat >"$BIN_DIR/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${DOCKER_ARGS:-/dev/null}"
exit 0
EOF
cat >"$BIN_DIR/curl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${CURL_ARGS:-/dev/null}"
exit 0
EOF
chmod +x "$BIN_DIR/docker" "$BIN_DIR/curl"

# The stand-in for `go test`: records the two URLs it was handed.
cat >"$WORK/fake-go-test.sh" <<'EOF'
#!/usr/bin/env bash
printf 'PG=%s\nVM=%s\n' "${POSTGRES_TEST_URL:-unset}" "${VICTORIAMETRICS_TEST_URL:-unset}" >"$SEEN_ENV"
exit "${FAKE_GO_TEST_STATUS:-0}"
EOF
chmod +x "$WORK/fake-go-test.sh"

run_provision() {
  (
    export PATH="$BIN_DIR:$PATH"
    export DOCKER_ARGS="$WORK/docker.args"
    export CURL_ARGS="$WORK/curl.args"
    export SEEN_ENV="$WORK/seen-env.txt"
    export GO_TEST_CMD="$WORK/fake-go-test.sh"
    "$PROVISION"
  )
}

echo "provisioning:"
: >"$WORK/docker.args"
: >"$WORK/seen-env.txt"
rc=0
(
  unset POSTGRES_TEST_URL VICTORIAMETRICS_TEST_URL
  run_provision
) >/dev/null 2>&1 || rc=$?
assert_eq "exits 0 when the suite passes" "0" "$rc"

DOCKER_ARGS_TEXT="$(cat "$WORK/docker.args")"
assert_contains "starts a Postgres container" "$DOCKER_ARGS_TEXT" "postgres:17-alpine"
assert_contains "starts a VictoriaMetrics container" "$DOCKER_ARGS_TEXT" "victoriametrics/victoria-metrics"
assert_eq "starts exactly one Postgres" "1" "$(grep -c 'run -d .*postgres:17-alpine' "$WORK/docker.args" || true)"
assert_eq "starts exactly one VictoriaMetrics" "1" "$(grep -c 'run -d .*victoria-metrics' "$WORK/docker.args" || true)"

SEEN="$(cat "$WORK/seen-env.txt")"
assert_contains "exports POSTGRES_TEST_URL to the suite" "$SEEN" "PG=postgres://"
assert_contains "exports VICTORIAMETRICS_TEST_URL to the suite" "$SEEN" "VM=http://"

echo
echo "stale-container clear:"
# A crashed previous run can leave a container holding the port, so each start
# clears its name first.
assert_eq "clears a stale Postgres container before starting" "rm -f opengate-pg-test" \
  "$(head -n 1 "$WORK/docker.args")"

echo
echo "teardown:"
# Both containers must be gone on the way out, and the removals must be the LAST
# thing the run does — a mid-run count would also be satisfied by the stale clear.
TAIL2="$(tail -n 2 "$WORK/docker.args")"
assert_contains "removes the Postgres container on exit" "$TAIL2" "rm -f opengate-pg-test"
assert_contains "removes the VictoriaMetrics container on exit" "$TAIL2" "rm -f opengate-vm-test"

: >"$WORK/docker.args"
rc=0
(
  unset POSTGRES_TEST_URL VICTORIAMETRICS_TEST_URL
  FAKE_GO_TEST_STATUS=7 run_provision
) >/dev/null 2>&1 || rc=$?
assert_eq "propagates the suite's exit code" "7" "$rc"
TAIL2="$(tail -n 2 "$WORK/docker.args")"
assert_contains "removes Postgres even when the suite fails" "$TAIL2" "rm -f opengate-pg-test"
assert_contains "removes VictoriaMetrics even when the suite fails" "$TAIL2" "rm -f opengate-vm-test"

echo
echo "external URLs win:"
# A caller that already has a stack (CI, or a long-lived local one) must pay no
# container cost and keep its own URLs.
: >"$WORK/docker.args"
: >"$WORK/seen-env.txt"
rc=0
(
  export POSTGRES_TEST_URL="postgres://ext:ext@127.0.0.1:15432/ext?sslmode=disable"
  export VICTORIAMETRICS_TEST_URL="http://127.0.0.1:18428"
  run_provision
) >/dev/null 2>&1 || rc=$?
assert_eq "exits 0 with external URLs" "0" "$rc"
assert_eq "starts no container when both URLs are supplied" "0" \
  "$(grep -c 'run -d' "$WORK/docker.args" || true)"
SEEN="$(cat "$WORK/seen-env.txt")"
assert_contains "passes the external Postgres URL through" "$SEEN" "PG=postgres://ext:ext@127.0.0.1:15432"
assert_contains "passes the external VictoriaMetrics URL through" "$SEEN" "VM=http://127.0.0.1:18428"

echo
echo "image pins match the Go harness:"
# The provisioner and the in-process fallback must agree, or a developer running
# `make test-go` and one running a bare `go test` exercise different versions.
GO_PG_IMAGE="$(grep -oE 'PostgresImage = "[^"]+"' "$ROOT/server/internal/testpg/testpg.go" | cut -d'"' -f2)"
SH_PG_IMAGE="$(grep -oE '^PG_IMAGE="[^"]+"' "$PROVISION" | cut -d'"' -f2)"
assert_eq "Postgres image pin matches testpg.PostgresImage" "$GO_PG_IMAGE" "$SH_PG_IMAGE"

GO_VM_IMAGE="$(grep -oE 'image = "victoriametrics/[^"]+"' "$ROOT/server/internal/testvm/testvm.go" | cut -d'"' -f2)"
SH_VM_IMAGE="$(grep -oE '^VM_IMAGE="[^"]+"' "$PROVISION" | cut -d'"' -f2)"
assert_eq "VictoriaMetrics image pin matches testvm's image" "$GO_VM_IMAGE" "$SH_VM_IMAGE"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
