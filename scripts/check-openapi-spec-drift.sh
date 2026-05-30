#!/usr/bin/env bash
# check-openapi-spec-drift.sh — Rule 6 of the pen-test gate (ADR-027).
#
# Semgrep cannot span the YAML→Go boundary, so this script wraps that check.
#
# In OpenGate auth is applied CENTRALLY: oapiAuthMiddleware() (api.go) reads the
# generated BearerAuthScopes context key — which oapi-codegen derives from each
# operation's `security: [bearerAuth]` block in api/openapi.yaml — and only then
# runs AuthMiddleware. A handler therefore cannot "forget" the middleware; the
# spec is the single source of truth. The real drift risk is the inverse: a
# *mutating* operation (POST/PUT/PATCH/DELETE) added to the spec WITHOUT a
# security block, silently shipping a publicly-reachable write endpoint.
#
# This script flags every mutating operation that has no `security:` block,
# minus an explicit allowlist of operations that are public BY DESIGN
# (unauthenticated bootstrap: register, login, agent enroll).
#
# Pure line-scan — no PyYAML dependency (the semgrep venv and CI runners may
# lack it). Exit 0 = no drift. Exit 1 = at least one unguarded mutating op.
set -euo pipefail

SPEC="${1:-api/openapi.yaml}"

if [ ! -f "$SPEC" ]; then
  echo "check-openapi-spec-drift: spec not found at $SPEC" >&2
  exit 1
fi

# Operations that are intentionally public. Each MUST be justified here; adding
# an entry is a security decision reviewed under ADR-027.
#   register / login — pre-auth credential exchange (no token exists yet)
#   enroll           — agent bootstrap, authorized by a single-use enrollment
#                      token in the URL path, not a JWT
ALLOWED_PUBLIC_MUTATIONS="register login enroll"

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
