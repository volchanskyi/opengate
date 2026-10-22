#!/usr/bin/env bash
# Tests for scripts/authority-job.sh: one key-tool command runs as a job on the running server's
# image, its output is printed, and every object it made is deleted whatever the outcome.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNNER="$REPO_ROOT/scripts/authority-job.sh"
[ -x "$RUNNER" ] || {
  echo "FAIL: $RUNNER not executable" >&2
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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

cat >"$WORK/bin/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
# Stand-in for kubectl: records each call, keeps what was applied, answers the job's status.
set -uo pipefail
printf '%s\n' "$*" >>"$FAKE_TRACE"
args=" $* "
case "$args" in
  *" get deploy "*)
    if [ -n "${FAKE_IMAGE:-}" ]; then printf '%s' "$FAKE_IMAGE"; else
      echo 'Error from server (NotFound): deployments.apps "opengate-server" not found' >&2
      exit 1
    fi
    ;;
  *" apply "*)
    file=""
    while [ "$#" -gt 0 ]; do
      [ "$1" = "-f" ] && file="$2"
      shift
    done
    cp "$file" "$FAKE_APPLIED"
    echo "job.batch/opengate-authority-tool created"
    ;;
  *" get job.batch/"*)
    printf '%s' "$FAKE_STATUS"
    ;;
  *" logs "*)
    echo "opengate-authority:"
    echo "  issuer  \"OpenGate CA\"  0a1b2c3d4e5f  expires 2036-03-15"
    ;;
  *" delete "*) ;;
  *)
    echo "unexpected kubectl call: $*" >&2
    exit 64
    ;;
esac
FAKE_KUBECTL
chmod +x "$WORK/bin/kubectl"

# run executes the runner with the stand-in on PATH; extra environment comes as NAME=value words.
run() {
  : >"$WORK/trace"
  rm -f "$WORK/applied.yaml"
  env PATH="$WORK/bin:$PATH" FAKE_TRACE="$WORK/trace" FAKE_APPLIED="$WORK/applied.yaml" \
    AUTHORITY_JOB_POLL=0 FAKE_STATUS="1/" FAKE_IMAGE="ghcr.io/volchanskyi/opengate-server:sha-abc" \
    "${ENV_EXTRA[@]}" bash "$RUNNER" "$@"
}

# applied prints one field of the Job the runner applied.
applied() {
  python3 -c '
import json, sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1])):
    if d and d["kind"] == "Job":
        c = d["spec"]["template"]["spec"]["containers"][0]
        print({"image": c["image"], "args": " ".join(c["args"])}[sys.argv[2]])
' "$WORK/applied.yaml" "$1"
}

echo "authority-job:"

ENV_EXTRA=()
status=0
out="$(run layout -phase changeover 2>&1)" || status=$?
trace="$(cat "$WORK/trace")"
assert_eq "a finished command exits 0" "0" "$status"
assert_eq "the job runs on the running server's image" \
  "ghcr.io/volchanskyi/opengate-server:sha-abc" "$(applied image)"
assert_eq "the job runs the command it was given" \
  "layout -secret opengate-authority -phase changeover" "$(applied args)"
if grep -qF "0a1b2c3d4e5f" <<<"$out"; then pass "the job's output is printed"; else fail "the job's output is printed: $out"; fi
last="$(tail -n 1 <<<"$trace")"
if grep -qE '^-n opengate delete -f .* --ignore-not-found' <<<"$last"; then
  pass "every object the run made is deleted afterwards"
else
  fail "every object the run made is deleted afterwards (last call: $last)"
fi
first_apply="$(awk '/ apply /{print NR; exit}' <<<"$trace")"
first_delete="$(awk '/ delete /{print NR; exit}' <<<"$trace")"
if [ -n "$first_delete" ] && [ -n "$first_apply" ] && [ "$first_delete" -lt "$first_apply" ]; then
  pass "a job left by an earlier run is cleared before this one is applied"
else
  fail "a job left by an earlier run is cleared before this one is applied"
fi

ENV_EXTRA=(FAKE_STATUS="/1")
status=0
out="$(run new-kit 2>&1)" || status=$?
assert_eq "a failed command exits 1" "1" "$status"
if grep -qF "0a1b2c3d4e5f" <<<"$out" && grep -qF " delete " "$WORK/trace"; then
  pass "a failed command still prints its output and is deleted"
else
  fail "a failed command still prints its output and is deleted: $out"
fi

ENV_EXTRA=(FAKE_STATUS="/" AUTHORITY_JOB_TIMEOUT=0)
status=0
out="$(run inspect 2>&1)" || status=$?
assert_eq "a job that never finishes fails" "1" "$status"
if grep -qF "did not finish" <<<"$out" && grep -qF " delete " "$WORK/trace"; then
  pass "an unfinished job says so and is deleted"
else
  fail "an unfinished job says so and is deleted: $out"
fi

ENV_EXTRA=(FAKE_IMAGE=)
status=0
out="$(run init 2>&1)" || status=$?
assert_eq "with no running server and no IMAGE_TAG it refuses" "2" "$status"
if [ ! -e "$WORK/applied.yaml" ] && grep -qF "IMAGE_TAG" <<<"$out"; then
  pass "the refusal applies nothing and names IMAGE_TAG"
else
  fail "the refusal applies nothing and names IMAGE_TAG: $out"
fi

ENV_EXTRA=(FAKE_IMAGE= IMAGE_TAG=v9.9.9)
status=0
run init >/dev/null 2>&1 || status=$?
assert_eq "IMAGE_TAG names the image on a cluster with no server yet" \
  "0 ghcr.io/volchanskyi/opengate-server:v9.9.9" "$status $(applied image)"

ENV_EXTRA=()
status=0
out="$(run 2>&1)" || status=$?
assert_eq "no command is a usage error" "2" "$status"
if grep -qF "Usage:" <<<"$out"; then pass "the usage is printed"; else fail "the usage is printed: $out"; fi

echo
echo "authority-job: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
