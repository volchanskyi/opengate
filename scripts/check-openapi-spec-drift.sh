#!/usr/bin/env bash
# Flags every mutating operation in the spec that has no `security:` block, bar an allowlist.
# Auth is applied from each operation's security block, so a missing block ships a public write.
#
# Exit codes:
#   0  no drift
#   1  at least one unguarded mutating operation
set -euo pipefail

SPEC="${1:-api/openapi.yaml}"

if [ ! -f "$SPEC" ]; then
  echo "check-openapi-spec-drift: spec not found at $SPEC" >&2
  exit 1
fi

# Operations that are public by design: pre-auth register and login, enroll (single-use
# token in the path) and browser crash reporting (size-bounded, rate-limited, write-only).
ALLOWED_PUBLIC_MUTATIONS="register login enroll reportClientError"

python3 - "$SPEC" "$ALLOWED_PUBLIC_MUTATIONS" <<'PY'
import re, sys

spec_path, allowed = sys.argv[1], set(sys.argv[2].split())
lines = open(spec_path).read().split("\n")

path_re   = re.compile(r'^  (/[^\s:]*):\s*$')
method_re = re.compile(r'^    (get|post|put|patch|delete):\s*$')
sec_re    = re.compile(r'^      security:\s*$')

MUTATING = {"POST", "PUT", "PATCH", "DELETE"}
findings = []
cur_path = None

for idx, line in enumerate(lines):
    pm = path_re.match(line)
    if pm:
        cur_path = pm.group(1)
        continue
    mm = method_re.match(line)
    if not mm:
        continue
    method = mm.group(1).upper()
    opid, has_sec = None, False
    j = idx + 1
    while j < len(lines):
        nl = lines[j]
        if method_re.match(nl) or path_re.match(nl):
            break
        if opid is None and "operationId:" in nl:
            opid = nl.split("operationId:")[1].strip()
        if sec_re.match(nl):
            has_sec = True
        j += 1
    if method in MUTATING and not has_sec:
        if opid not in allowed:
            findings.append((method, cur_path, opid))

if findings:
    print("OpenAPI spec drift — mutating operation(s) with NO security block:", file=sys.stderr)
    for m, p, o in findings:
        print(f"  {m} {p}  (operationId={o})", file=sys.stderr)
    print("", file=sys.stderr)
    print("A POST/PUT/PATCH/DELETE without `security: [bearerAuth]` is publicly", file=sys.stderr)
    print("reachable. Add the security block, or — if intentionally public —", file=sys.stderr)
    print("add its operationId to ALLOWED_PUBLIC_MUTATIONS in", file=sys.stderr)
    print("scripts/check-openapi-spec-drift.sh with a justification. ADR-027.", file=sys.stderr)
    sys.exit(1)

print("check-openapi-spec-drift: no drift (all mutating ops are secured or allowlisted).", file=sys.stderr)
sys.exit(0)
PY
