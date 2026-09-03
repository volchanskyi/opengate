#!/usr/bin/env bash
# Behavioural tests for the test-value analyser and the hook that fronts it,
# plus the repo-wide sweep that holds the same line on every tracked web test.
#
# The two patterns under test are the two the census could show had each cost
# something real, and neither of which fires on a working test:
#
#   1. A test file that never binds the primary export of the module it is
#      named for. web/src/lib/api.test.ts copied the production auth middleware
#      into the test and asserted on the copy, so removing api.use(...) left
#      every browser request unauthenticated with both tests still green.
#
#   2. A global or prototype reassignment with no restore, which makes a test
#      pass or fail on what ran before it. The layout override in
#      DeviceList.test.tsx restores inside a finally and is correct code.
#
# Run: ./scripts/tests/test-value.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$ROOT/scripts/test-value-check.sh"
HOOK="$ROOT/.claude/hooks/pretooluse-test-value-guard.sh"

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

echo "test-value:"

for required in "$CHECK" "$HOOK"; do
  if [ ! -x "$required" ]; then
    fail "missing or non-executable: ${required#"$ROOT/"}"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

# ------------------------------------------------------------------
# A fixture tree, so the analyser is exercised against files that are
# not in the repository and cannot drift with it.
# ------------------------------------------------------------------
FIXTURES="$(mktemp -d)"
trap 'rm -rf "$FIXTURES"' EXIT

mkdir -p "$FIXTURES/web/src/lib" "$FIXTURES/web/src/features"

cat >"$FIXTURES/web/src/lib/client.ts" <<'EOF'
export const QUERY_SERIALIZER = { array: { style: 'form' } } as const;
export const client = createClient({ baseUrl: '' });
client.use(authMiddleware);
EOF

cat >"$FIXTURES/web/src/lib/helpers.ts" <<'EOF'
export function formatOne(n: number): string {
  return String(n);
}
EOF

cat >"$FIXTURES/web/src/features/Panel.tsx" <<'EOF'
export function Panel() {
  return null;
}
EOF

# Analyse one fixture file. Sets CHECK_EXIT and CHECK_STDERR.
run_check() {
  local label="$1"
  local stderr_file
  stderr_file="$(mktemp)"
  CHECK_EXIT=0
  if TEST_VALUE_ROOT="$FIXTURES" "$CHECK" file "$label" >/dev/null 2>"$stderr_file"; then
    CHECK_EXIT=0
  else
    CHECK_EXIT=$?
  fi
  CHECK_STDERR="$(cat "$stderr_file")"
  rm -f "$stderr_file"
}

assert_check() {
  local name="$1" label="$2" expected="$3"
  run_check "$label"
  if [ "$CHECK_EXIT" = "$expected" ]; then
    pass "$name (exit $expected)"
  else
    fail "$name (expected exit $expected, got $CHECK_EXIT; $(printf '%s' "$CHECK_STDERR" | head -1))"
  fi
}

assert_check_stderr() {
  local name="$1" needle="$2"
  if printf '%s' "$CHECK_STDERR" | grep -qF "$needle"; then
    pass "$name"
  else
    fail "$name (stderr missing '$needle'; got: $(printf '%s' "$CHECK_STDERR" | head -1))"
  fi
}

# ------------------------------------------------------------------
# 1. The primary export the module is named for
# ------------------------------------------------------------------
echo
echo "## primary export"

# The api.test.ts shape: imports a secondary export, copies the rest.
cat >"$FIXTURES/web/src/lib/client.test.ts" <<'EOF'
import { describe, it, expect } from 'vitest';
import { QUERY_SERIALIZER } from './client';

describe('client', () => {
  it('attaches the header', () => {
    const middleware = { onRequest: () => undefined };
    expect(middleware).toBeTruthy();
    expect(QUERY_SERIALIZER).toBeTruthy();
  });
});
EOF
assert_check "test importing only a secondary export: refused" web/src/lib/client.test.ts 1
assert_check_stderr "refusal names the unbound primary export" "client"

# Binding the primary export clears it.
cat >"$FIXTURES/web/src/lib/client.test.ts" <<'EOF'
import { describe, it, expect } from 'vitest';
import { client, QUERY_SERIALIZER } from './client';

describe('client', () => {
  it('is built with the shared serializer', () => {
    expect(client).toBeTruthy();
    expect(QUERY_SERIALIZER).toBeTruthy();
  });
});
EOF
assert_check "test binding the primary export: allowed" web/src/lib/client.test.ts 0

# A namespace import reaches every export, including the primary one.
cat >"$FIXTURES/web/src/lib/client.test.ts" <<'EOF'
import { describe, it, expect } from 'vitest';
import * as mod from './client';

describe('client', () => {
  it('is built with the shared serializer', () => {
    expect(mod.client).toBeTruthy();
  });
});
EOF
assert_check "namespace import: allowed" web/src/lib/client.test.ts 0
rm -f "$FIXTURES/web/src/lib/client.test.ts"

# A hyphenated module name resolves to its camel-case export.
cat >"$FIXTURES/web/src/lib/format-one.ts" <<'EOF'
export function formatOne(n: number): string {
  return String(n);
}
EOF
cat >"$FIXTURES/web/src/lib/format-one.test.ts" <<'EOF'
import { it, expect } from 'vitest';
import { formatOne } from './format-one';

it('formats', () => {
  expect(formatOne(1)).toBe('1');
});
EOF
assert_check "hyphenated module, camel-case export bound: allowed" web/src/lib/format-one.test.ts 0

cat >"$FIXTURES/web/src/lib/format-one.test.ts" <<'EOF'
import { it, expect } from 'vitest';

function formatOne(n: number): string {
  return String(n);
}

it('formats', () => {
  expect(formatOne(1)).toBe('1');
});
EOF
assert_check "hyphenated module, export copied into the test: refused" web/src/lib/format-one.test.ts 1
rm -f "$FIXTURES/web/src/lib/format-one.ts" "$FIXTURES/web/src/lib/format-one.test.ts"

# A component module: the export carries the file's own name.
cat >"$FIXTURES/web/src/features/Panel.test.tsx" <<'EOF'
import { it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { Panel } from './Panel';

it('renders', () => {
  expect(render(<Panel />)).toBeTruthy();
});
EOF
assert_check "component test binding its component: allowed" web/src/features/Panel.test.tsx 0
rm -f "$FIXTURES/web/src/features/Panel.test.tsx"

# No sibling module of that name: nothing to assert, so nothing is refused.
cat >"$FIXTURES/web/src/lib/nothing-beside-it.test.ts" <<'EOF'
import { it, expect } from 'vitest';

it('holds', () => {
  expect(1).toBe(1);
});
EOF
assert_check "test with no sibling module: allowed" web/src/lib/nothing-beside-it.test.ts 0
rm -f "$FIXTURES/web/src/lib/nothing-beside-it.test.ts"

# A module whose exports are all named something else: no primary export to bind.
cat >"$FIXTURES/web/src/lib/helpers.test.ts" <<'EOF'
import { it, expect } from 'vitest';
import { formatOne } from './helpers';

it('formats', () => {
  expect(formatOne(1)).toBe('1');
});
EOF
assert_check "module with no same-named export: allowed" web/src/lib/helpers.test.ts 0
rm -f "$FIXTURES/web/src/lib/helpers.test.ts"

# ------------------------------------------------------------------
# 2. Global and prototype reassignment
# ------------------------------------------------------------------
echo
echo "## un-restored global and prototype reassignment"

cat >"$FIXTURES/web/src/lib/layout.test.ts" <<'EOF'
import { it, expect } from 'vitest';

it('measures at the breakpoint', () => {
  Element.prototype.getBoundingClientRect = () => ({ width: 768 }) as DOMRect;
  expect(document.body.getBoundingClientRect().width).toBe(768);
});
EOF
assert_check "prototype reassignment with no restore: refused" web/src/lib/layout.test.ts 1
assert_check_stderr "refusal names the reassigned target" "Element.prototype.getBoundingClientRect"

cat >"$FIXTURES/web/src/lib/layout.test.ts" <<'EOF'
import { it, expect } from 'vitest';

it('measures at the breakpoint', () => {
  const original = Element.prototype.getBoundingClientRect;
  try {
    Element.prototype.getBoundingClientRect = () => ({ width: 768 }) as DOMRect;
    expect(document.body.getBoundingClientRect().width).toBe(768);
  } finally {
    Element.prototype.getBoundingClientRect = original;
  }
});
EOF
assert_check "prototype reassignment restored in a finally: allowed" web/src/lib/layout.test.ts 0

cat >"$FIXTURES/web/src/lib/layout.test.ts" <<'EOF'
import { it, expect, afterEach } from 'vitest';

const original = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = original;
});

it('answers from the stub', async () => {
  globalThis.fetch = async () => new Response('{}');
  expect(await (await globalThis.fetch('/x')).text()).toBe('{}');
});
EOF
assert_check "global reassignment restored in afterEach: allowed" web/src/lib/layout.test.ts 0

cat >"$FIXTURES/web/src/lib/layout.test.ts" <<'EOF'
import { it, expect } from 'vitest';

globalThis.fetch = async () => new Response('{}');

it('answers from the stub', async () => {
  expect(await (await globalThis.fetch('/x')).text()).toBe('{}');
});
EOF
assert_check "global reassignment with no restore: refused" web/src/lib/layout.test.ts 1

# Comparison is not assignment.
cat >"$FIXTURES/web/src/lib/layout.test.ts" <<'EOF'
import { it, expect } from 'vitest';

it('reads the global', () => {
  expect(globalThis.fetch === undefined).toBe(false);
  expect(window.location.pathname).toBe('/');
});
EOF
assert_check "reading a global: allowed" web/src/lib/layout.test.ts 0
rm -f "$FIXTURES/web/src/lib/layout.test.ts"

# A non-test file is out of scope even when it assigns a global.
cat >"$FIXTURES/web/src/lib/setup.ts" <<'EOF'
globalThis.fetch = async () => new Response('{}');
EOF
assert_check "non-test file assigning a global: out of scope" web/src/lib/setup.ts 0
rm -f "$FIXTURES/web/src/lib/setup.ts"

# ------------------------------------------------------------------
# 3. The hook in front of the analyser
# ------------------------------------------------------------------
echo
echo "## pretooluse-test-value-guard.sh"

build_envelope() {
  local tool="$1" input_json="$2"
  python3 - "$tool" "$input_json" <<'PYEOF'
import json, sys
tool, input_json = sys.argv[1], sys.argv[2]
print(json.dumps({
    "session_id": "test-session",
    "cwd": ".",
    "hook_event_name": "PreToolUse",
    "tool_name": tool,
    "tool_input": json.loads(input_json),
}))
PYEOF
}

run_hook() {
  local envelope="$1"
  local stderr_file
  stderr_file="$(mktemp)"
  HOOK_EXIT=0
  if printf '%s' "$envelope" | TEST_VALUE_ROOT="$FIXTURES" "$HOOK" >/dev/null 2>"$stderr_file"; then
    HOOK_EXIT=0
  else
    HOOK_EXIT=$?
  fi
  HOOK_STDERR="$(cat "$stderr_file")"
  rm -f "$stderr_file"
}

assert_hook() {
  local name="$1" expected="$2"
  if [ "$HOOK_EXIT" = "$expected" ]; then
    pass "$name (exit $expected)"
  else
    fail "$name (expected exit $expected, got $HOOK_EXIT; $(printf '%s' "$HOOK_STDERR" | head -1))"
  fi
}

copied=$(printf 'import { QUERY_SERIALIZER } from "./client";\nconst authMiddleware = {};\nit("x", () => { expect(authMiddleware).toBeTruthy(); });\n')
envelope="$(build_envelope Write "$(python3 -c '
import json,sys
print(json.dumps({"file_path": "web/src/lib/client.test.ts", "content": sys.argv[1]}))
' "$copied")")"
run_hook "$envelope"
assert_hook "Write of a test that copies the module it is named for: BLOCK" 2
if printf '%s' "$HOOK_STDERR" | grep -qF "test-value.md"; then
  pass "hook refusal cites the rule"
else
  fail "hook refusal does not cite the rule (got: $(printf '%s' "$HOOK_STDERR" | head -1))"
fi

bound=$(printf 'import { client } from "./client";\nit("x", () => { expect(client).toBeTruthy(); });\n')
envelope="$(build_envelope Write "$(python3 -c '
import json,sys
print(json.dumps({"file_path": "web/src/lib/client.test.ts", "content": sys.argv[1]}))
' "$bound")")"
run_hook "$envelope"
assert_hook "Write of a test binding the primary export: allow" 0

# An Edit is judged on the file the edit produces, not on the fragment.
cat >"$FIXTURES/web/src/lib/client.test.ts" <<'EOF'
import { it, expect } from 'vitest';
import { client } from './client';

it('exists', () => {
  expect(client).toBeTruthy();
});
EOF
envelope="$(build_envelope Edit '{"file_path":"web/src/lib/client.test.ts","old_string":"expect(client).toBeTruthy();","new_string":"expect(client).toBeDefined();"}')"
run_hook "$envelope"
assert_hook "Edit keeping the primary export bound: allow" 0

envelope="$(build_envelope Edit '{"file_path":"web/src/lib/client.test.ts","old_string":"import { client } from '"'"'./client'"'"';","new_string":"const client = {};"}')"
run_hook "$envelope"
assert_hook "Edit removing the import and copying the module: BLOCK" 2
rm -f "$FIXTURES/web/src/lib/client.test.ts"

envelope="$(build_envelope Bash '{"command":"npx vitest run","description":"run"}')"
run_hook "$envelope"
assert_hook "Bash tool: allow (not a write)" 0

envelope="$(build_envelope Write '{"file_path":"docs/infrastructure/Testing.md","content":"globalThis.fetch = stub"}')"
run_hook "$envelope"
assert_hook "Markdown describing the patterns: allow" 0

# ------------------------------------------------------------------
# 4. The repo-wide sweep
# ------------------------------------------------------------------
echo
echo "## repo sweep"

sweep_stderr="$(mktemp)"
sweep_exit=0
if "$CHECK" sweep >/dev/null 2>"$sweep_stderr"; then
  sweep_exit=0
else
  sweep_exit=$?
fi
if [ "$sweep_exit" = 0 ]; then
  pass "every tracked web test clears the analyser"
else
  fail "repo sweep found violations:
$(cat "$sweep_stderr")"
fi
rm -f "$sweep_stderr"

echo
printf 'Summary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
