#!/usr/bin/env bash
# Tests for scripts/docker-credstore-guard.sh. Plain bash; no bats.
# Run: ./scripts/tests/docker-credstore-guard.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GUARD="$PROJECT_ROOT/scripts/docker-credstore-guard.sh"

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

# Each case runs the guard with an isolated DOCKER_CONFIG + XDG_CACHE_HOME + PATH.
# Sets OUT (stdout) and RC (exit code).
run_guard() {
  local docker_config="$1" cache="$2" extra_path="${3:-}"
  RC=0
  OUT="$(DOCKER_CONFIG="$docker_config" XDG_CACHE_HOME="$cache" \
    PATH="${extra_path:+$extra_path:}$PATH" "$GUARD" 2>/dev/null)" || RC=$?
}

echo "docker-credstore-guard:"

# 1. No config.json → echoes the src dir unchanged, exit 0.
TMP="$(mktemp -d)"
run_guard "$TMP/dcfg" "$TMP/cache"
if [ "$RC" = 0 ] && [ "$OUT" = "$TMP/dcfg" ]; then
  pass "no config.json: src dir unchanged"
else fail "no config.json (rc=$RC out=$OUT want=$TMP/dcfg)"; fi
rm -rf "$TMP"

# 2. config.json without credsStore → src dir unchanged.
TMP="$(mktemp -d)"
mkdir -p "$TMP/dcfg"
printf '{"proxies":{}}' >"$TMP/dcfg/config.json"
run_guard "$TMP/dcfg" "$TMP/cache"
if [ "$RC" = 0 ] && [ "$OUT" = "$TMP/dcfg" ]; then
  pass "no credsStore: src dir unchanged"
else fail "no credsStore (rc=$RC out=$OUT)"; fi
rm -rf "$TMP"

# 3. Working credsStore helper → src dir unchanged (helper probe passes).
TMP="$(mktemp -d)"
mkdir -p "$TMP/dcfg" "$TMP/bin"
printf '{"credsStore":"works"}' >"$TMP/dcfg/config.json"
printf '#!/usr/bin/env bash\nexit 0\n' >"$TMP/bin/docker-credential-works"
chmod +x "$TMP/bin/docker-credential-works"
run_guard "$TMP/dcfg" "$TMP/cache" "$TMP/bin"
if [ "$RC" = 0 ] && [ "$OUT" = "$TMP/dcfg" ]; then
  pass "working helper: src dir unchanged"
else fail "working helper (rc=$RC out=$OUT)"; fi
rm -rf "$TMP"

# 4. Missing credsStore helper → sanitized dir; config.json has no credsStore.
TMP="$(mktemp -d)"
mkdir -p "$TMP/dcfg"
printf '{"credsStore":"opengate-test-guaranteed-missing-7f4c9d"}' >"$TMP/dcfg/config.json"
run_guard "$TMP/dcfg" "$TMP/cache"
if [ "$RC" = 0 ] && [ "$OUT" != "$TMP/dcfg" ] && [ -f "$OUT/config.json" ] \
  && ! grep -q credsStore "$OUT/config.json"; then
  pass "missing helper: sanitized dir without credsStore"
else
  fail "missing helper (rc=$RC out=$OUT cfg=$(cat "$OUT/config.json" 2>/dev/null))"
fi
rm -rf "$TMP"

# 5. Broken helper (present but fails) → sanitized; other keys preserved.
TMP="$(mktemp -d)"
mkdir -p "$TMP/dcfg" "$TMP/bin"
printf '{"credsStore":"broken","auths":{"x":{}},"proxies":{"default":{}}}' >"$TMP/dcfg/config.json"
printf '#!/usr/bin/env bash\nexit 1\n' >"$TMP/bin/docker-credential-broken"
chmod +x "$TMP/bin/docker-credential-broken"
run_guard "$TMP/dcfg" "$TMP/cache" "$TMP/bin"
if [ "$RC" = 0 ] && [ "$OUT" != "$TMP/dcfg" ] \
  && ! grep -q credsStore "$OUT/config.json" \
  && grep -q '"proxies"' "$OUT/config.json" && grep -q '"auths"' "$OUT/config.json"; then
  pass "broken helper: sanitized, other keys preserved"
else
  fail "broken helper (rc=$RC out=$OUT cfg=$(cat "$OUT/config.json" 2>/dev/null))"
fi
rm -rf "$TMP"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
