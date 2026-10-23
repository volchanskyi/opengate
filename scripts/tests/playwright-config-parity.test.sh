#!/usr/bin/env bash
# The staging Playwright config overrides only the base URL, retries and webServer of the local one,
# and narrows the local projects to the specs a deployed release can run.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOCAL_CFG="$REPO_ROOT/web/playwright.config.ts"
STAGING_CFG="$REPO_ROOT/web/playwright.staging.config.ts"

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

echo "playwright config parity:"

for f in "$LOCAL_CFG" "$STAGING_CFG"; do
  if [ ! -f "$f" ]; then
    printf '  FAIL missing config: %s\n' "$f" >&2
    exit 1
  fi
done

if grep -qE '^import .* from "\./playwright\.config"' "$STAGING_CFG"; then
  pass "staging config imports ./playwright.config"
else
  fail "staging config does not import ./playwright.config — it must derive from it, not fork it"
fi

INHERITED_KEYS=(workers globalSetup globalTeardown timeout fullyParallel testDir)
for key in "${INHERITED_KEYS[@]}"; do
  if grep -qE "^[[:space:]]*${key}:" "$STAGING_CFG"; then
    fail "staging config redeclares '${key}' — it must be inherited from playwright.config.ts"
  else
    pass "staging config inherits '${key}'"
  fi
done

# A config's projects are inherited, or built from the local config's own list.
projects_derived() {
  local line
  line="$(grep -E '^[[:space:]]*projects:' "$1" || true)"
  [ -z "$line" ] || grep -qE '^[[:space:]]*projects:[[:space:]]*(shared|local)\.projects' <<<"$line"
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
printf '  projects: [\n    { name: "chromium" },\n  ],\n' >"$work/forked.ts"
printf '  projects: shared.projects\n    ?.map((project) => project),\n' >"$work/derived.ts"
if ! projects_derived "$work/forked.ts" && projects_derived "$work/derived.ts"; then
  pass "a projects list written afresh is told apart from one built from the local list"
else
  fail "the projects check cannot tell a forked list from a derived one"
fi
if projects_derived "$STAGING_CFG"; then
  pass "staging config builds its projects from the local config's"
else
  fail "staging config writes its own projects — it must build them from playwright.config.ts's"
fi

if grep -qE '^[[:space:]]*workers:[[:space:]]*1,' "$LOCAL_CFG"; then
  pass "local config pins workers: 1"
else
  fail "local config no longer pins 'workers: 1' — staging inherits it, so both runs would go parallel"
fi

if grep -qE 'baseURL:[[:space:]]*"http://127\.0\.0\.1:18080"' "$STAGING_CFG"; then
  pass "staging config targets the staging port-forward"
else
  fail "staging config does not target http://127.0.0.1:18080"
fi

# Prints every module under DIR that imports node:child_process, itself or through a chain of
# relative imports, one path relative to DIR per line.
reaches_local_program() {
  local dir="$1" found next module spec target candidate
  found="$(cd "$dir" && grep -rlE --include='*.ts' "from ['\"](node:)?child_process['\"]" -- * || true)"
  found="$(sort -u <<<"$found")"
  while :; do
    next="$found"
    while IFS= read -r module; do
      [[ $'\n'"$found"$'\n' == *$'\n'"$module"$'\n'* ]] && continue
      while IFS= read -r spec; do
        for candidate in "$spec.ts" "$spec" "$spec/index.ts"; do
          target="$(realpath -m --relative-to="$dir" "$dir/$(dirname "$module")/$candidate")"
          if [[ $'\n'"$found"$'\n' == *$'\n'"$target"$'\n'* ]]; then
            next="$next"$'\n'"$module"
            break 2
          fi
        done
      done < <(sed -nE "s/.*(from|import) ['\"](\.{1,2}\/[^'\"]+)['\"].*/\2/p" "$dir/$module")
    done < <(cd "$dir" && find . -name '*.ts' -printf '%P\n' | sort)
    next="$(grep -v '^$' <<<"$next" | sort -u)"
    [ "$next" = "$found" ] && break
    found="$next"
  done
  printf '%s\n' "$found"
}

mkdir -p "$work/e2e/helpers"
printf 'import { stop } from "./helpers/machine";\n' >"$work/e2e/reaches.spec.ts"
printf 'import {\n  run,\n} from "./shell";\nexport const stop = run;\n' >"$work/e2e/helpers/machine.ts"
printf 'import { execFileSync } from "node:child_process";\nexport const run = execFileSync;\n' \
  >"$work/e2e/helpers/shell.ts"
printf 'import { sign } from "./helpers/auth";\n' >"$work/e2e/plain.spec.ts"
printf 'export const sign = 1;\n' >"$work/e2e/helpers/auth.ts"
reached="$(reaches_local_program "$work/e2e")"
if [ "$reached" = "$(printf '%s\n' helpers/machine.ts helpers/shell.ts reaches.spec.ts)" ]; then
  pass "a spec reaching a local program through two helpers is found, and a plain spec is left"
else
  fail "the local-program reader found [$(tr '\n' ' ' <<<"$reached")] in the fixture"
fi

WEB="$REPO_ROOT/web"
if [ ! -x "$WEB/node_modules/.bin/playwright" ]; then
  printf '  FAIL Playwright is not installed in web/; run: (cd web && npm ci)\n' >&2
  exit 2
fi

# The spec files the staging run reaches, read from Playwright's own listing of that config.
if ! listing="$(cd "$WEB" && env -u PLAYWRIGHT_PERFORMANCE -u PLAYWRIGHT_ALL_BROWSERS \
  node_modules/.bin/playwright test --config=playwright.staging.config.ts --list --reporter=list 2>&1)"; then
  printf '%s\n' "$listing" >&2
  fail "Playwright could not list the staging run"
fi
staging_specs="$(sed -nE 's/^  \[[^]]+\] [^ ]+ ([^: ]+\.spec\.ts):[0-9]+:[0-9]+ .*/\1/p' <<<"$listing" | sort -u)"
all_specs="$(cd "$WEB/e2e" && find . -name '*.spec.ts' -printf '%P\n' | sort)"
local_modules="$(reaches_local_program "$WEB/e2e")"
local_specs="$(grep -E '\.spec\.ts$' <<<"$local_modules" || true)"

if [ -n "$staging_specs" ] && [ -n "$local_specs" ]; then
  pass "staging reaches $(wc -l <<<"$staging_specs") spec files; $(wc -l <<<"$local_specs") run a local program"
else
  fail "the staging listing or the local-program reader reached no spec file"
fi

misplaced=0
for spec in $all_specs; do
  runs=no
  local_only=no
  grep -qxF -- "$spec" <<<"$staging_specs" && runs=yes
  grep -qxF -- "$spec" <<<"$local_specs" && local_only=yes
  case "$runs$local_only" in
    yesyes) fail "$spec runs a local program, which staging lacks, and the staging run reaches it" ;;
    nono) fail "$spec runs nothing local, and the staging run leaves it out" ;;
    *) continue ;;
  esac
  misplaced=$((misplaced + 1))
done
if [ "$misplaced" -eq 0 ]; then
  pass "the staging run holds every spec that runs nothing local, and only those"
fi

printf '\n  %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '\nFailures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
