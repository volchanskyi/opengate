#!/usr/bin/env bash
# Each profile's load is recorded under its version, so a night is compared with nights of the same
# load: a change to the phases, fixture, safety or give-out rule fails here until the version rises.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RECORD="$SCRIPT_DIR/fixtures/profile-load-versions.json"

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

# unrecorded prints each profile in a directory whose load differs from what its version records.
unrecorded() { # profile-dir record
  python3 - "$1" "$2" <<'PY'
import glob, hashlib, json, sys

import yaml

LOAD = ("family", "environment", "fixture", "phases", "safety", "gave_out")
record = json.load(open(sys.argv[2], encoding="utf-8"))
read = 0
for path in sorted(glob.glob(sys.argv[1] + "/*.yaml")):
    doc = yaml.safe_load(open(path, encoding="utf-8")) or {}
    read += 1
    version = str(max(int(doc.get("version") or 1), 1))
    load = hashlib.sha256(json.dumps({k: doc.get(k) for k in LOAD}, sort_keys=True).encode()).hexdigest()[:16]
    recorded = record.get(doc.get("name"), {}).get(version)
    if recorded is None:
        print(f"{doc.get('name')} version {version} has no recorded load; record {load} for it")
    elif recorded != load:
        print(f"{doc.get('name')} version {version} is recorded as {recorded} and reads {load}; a change of load raises the version")
print(f"read={read}")
PY
}

echo "profile load versions:"

out="$(unrecorded "$REPO_ROOT/load/profiles" "$RECORD")"
read_count="$(sed -n 's/^read=//p' <<<"$out")"
problems="$(grep -v '^read=' <<<"$out" || true)"
if [ "${read_count:-0}" -lt 5 ]; then
  fail "read only ${read_count:-0} profile(s), so the record is checked against almost nothing"
elif [ -z "$problems" ]; then
  pass "each of $read_count profiles offers the load recorded for its version"
else
  fail "profiles whose load is not the one recorded for their version:"$'\n'"$problems"
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp "$REPO_ROOT/load/profiles/peak.yaml" "$WORK/peak.yaml"
sed -i 's/^    duration: 5m$/    duration: 6m/' "$WORK/peak.yaml"
if grep -qF 'version 1 is recorded as' <<<"$(unrecorded "$WORK" "$RECORD")"; then
  pass "demonstrated: a phase lengthened under the same version is refused"
else
  fail "demonstrated: a phase lengthened under the same version is refused"
fi
cp "$REPO_ROOT/load/profiles/peak.yaml" "$WORK/peak.yaml"
sed -i 's/^    max: 0.02$/    max: 0.03/' "$WORK/peak.yaml"
if [ -z "$(grep -v '^read=' <<<"$(unrecorded "$WORK" "$RECORD")" || true)" ]; then
  pass "demonstrated: a limit changed under the same version keeps its load"
else
  fail "demonstrated: a limit changed under the same version keeps its load"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
