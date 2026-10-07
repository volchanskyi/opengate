#!/usr/bin/env bash
# Holds the workflows to .claude/rules/ci-cd-determinism.md: a CI/CD step whose
# work was refused must not report success.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOWS="$REPO_ROOT/.github/workflows"
RULE="$REPO_ROOT/.claude/rules/ci-cd-determinism.md"
GUARD="scripts/assert-cache-written.sh"

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

echo "ci-cd-determinism:"

if [ -f "$RULE" ]; then
  pass "the rule exists"
else
  fail "the rule exists"
fi
if grep -qF 'rules/ci-cd-determinism.md' "$REPO_ROOT/CLAUDE.md"; then
  pass "the rule is indexed in CLAUDE.md"
else
  fail "the rule is not indexed in CLAUDE.md, so nothing points a reader at it"
fi
if [ -x "$REPO_ROOT/$GUARD" ]; then
  pass "the read-back guard is executable"
else
  fail "the read-back guard is executable"
fi

# The deploy token cannot write, so the deploy declares no cache of any kind.
CD="$WORKFLOWS/cd.yml"
for shape in 'actions/cache' 'Swatinem/rust-cache' 'cache-to:' '^[[:space:]]*cache:[[:space:]]'; do
  if grep -qE -- "$shape" "$CD"; then
    fail "cd.yml declares a cache ($shape); its token cannot write, so that save is refused and reported as a success"
  else
    pass "cd.yml declares no cache ($shape)"
  fi
done

# An inline save names its own key, so the step after it can assert that key.
SAVE_HITS=0
for wf in "$WORKFLOWS"/*.yml; do
  grep -qF 'actions/cache/save@' "$wf" || continue
  SAVE_HITS=$((SAVE_HITS + 1))
  name="$(basename "$wf")"
  if grep -qF "$GUARD" "$wf"; then
    pass "$name reads back the cache entry it writes"
  else
    fail "$name writes a cache entry and never reads it back"
  fi
done
if [ "$SAVE_HITS" -eq 0 ]; then
  pass "no workflow writes a cache entry inline"
fi

# The image workflow's cache keeps the agent cross-build off a cold start.
BUILD_IMAGE="$WORKFLOWS/build-image.yml"
if grep -qF "$GUARD" "$BUILD_IMAGE"; then
  pass "build-image reads back the agent build's cache"
else
  fail "build-image never reads back the agent build's cache, so a refusal there is silent"
fi

for target in x86_64-unknown-linux-musl aarch64-unknown-linux-musl; do
  if grep -qF "$target" "$BUILD_IMAGE"; then
    pass "the read-back covers $target"
  else
    fail "the read-back covers $target"
  fi
done

# A mutation shard that wrote no report fails where it ran, since the upload default only warns.
MUTATION="$WORKFLOWS/mutation.yml"
uploads="$(grep -c 'uses: actions/upload-artifact' "$MUTATION" || true)"
errors="$(grep -c 'if-no-files-found: error' "$MUTATION" || true)"
if [ "$uploads" -gt 0 ] && [ "$uploads" -eq "$errors" ]; then
  pass "all $uploads mutation.yml artifact uploads fail on an empty file set"
else
  fail "mutation.yml has $uploads artifact upload(s) but only $errors fail on an empty set — a shard that produced no report would report success"
fi

# `--locked` builds from the lockfile the tool's authors tested, not a fresh dependency graph.
install_bad=""
while IFS= read -r line; do
  case "$line" in
    *'#'*) continue ;;
  esac
  case "$line" in
    *'cargo install'*) ;;
    *) continue ;;
  esac
  case "$line" in
    *--locked*) ;;
    *) install_bad="$install_bad [$(printf '%s' "$line" | sed 's/^[[:space:]]*//')]" ;;
  esac
done < <(grep -rh 'cargo install' "$WORKFLOWS"/*.yml | sed 's/[[:space:]]*$//' | sort -u)
if [ -z "$install_bad" ]; then
  pass "every workflow cargo install pins its dependency graph with --locked"
else
  fail "cargo install without --locked re-resolves the tool's whole graph every run:$install_bad"
fi

# `gh api` infers POST from any field flag, and a list endpoint answers a POST with 404.
# This file is excluded because its own matcher strings carry the pattern it matches.
SELF="scripts/tests/ci-cd-determinism.test.sh"
GH_SOURCES=()
while IFS= read -r f; do
  [ "$f" = "$SELF" ] || GH_SOURCES+=("$REPO_ROOT/$f")
done < <(
  {
    git -C "$REPO_ROOT" ls-files '.github/workflows/*.yml'
    git -C "$REPO_ROOT" ls-files '*.sh'
  } | sort -u
)

gh_bad=""
gh_seen=0
gh_fielded=0
for f in "${GH_SOURCES[@]}"; do
  [ -f "$f" ] || continue
  grep -qF 'gh api' "$f" || continue
  while IFS= read -r entry; do
    where="${entry%%$'\t'*}"
    cmd="${entry#*$'\t'}"
    case "$cmd" in
      *'gh api'*) ;;
      *) continue ;;
    esac
    case "$cmd" in
      '#'*) continue ;;
    esac
    gh_seen=$((gh_seen + 1))
    grep -qE '(^|[[:space:]])(-f|-F|--field|--raw-field)([[:space:]=])' \
      <<<"$cmd" || continue
    gh_fielded=$((gh_fielded + 1))
    case "$cmd" in
      *'-X '* | *'--method '*) ;;
      *) gh_bad="$gh_bad"$'\n'"      $where" ;;
    esac
  done < <(awk '
    {
      line = $0
      sub(/^[[:space:]]+/, "", line)
      buf = buf (buf == "" ? "" : " ") line
      if (line ~ /\\$/) { sub(/\\$/, "", buf); next }
      print FILENAME ":" FNR "\t" buf
      buf = ""
    }
    END { if (buf != "") print FILENAME ":" FNR "\t" buf }
  ' "$f" | sed "s#^$REPO_ROOT/##")
done

if [ "$gh_seen" -eq 0 ]; then
  fail "the gh api sweep reached no call at all, so it is asserting an absence it never tested"
elif [ -z "$gh_bad" ]; then
  pass "of $gh_seen gh api calls, the $gh_fielded passing a field flag state their HTTP method"
else
  fail "gh api infers POST from a field flag, and a list endpoint answers a POST with 404:$gh_bad"
fi

# A required input is visible inline, through a wrapper, and per job plus the workflow-level env.
env_demo="$(mktemp -d)"
trap 'rm -rf "$env_demo"' EXIT

required_env_of() {
  local body
  body="$(grep -vE '^[[:space:]]*#' "$1" || true)"
  {
    grep -oE '\$\{[A-Z][A-Z0-9_]*:\?' <<<"$body" | grep -oE '[A-Z][A-Z0-9_]*' || true
    grep -E '^#[[:space:]]+[A-Z][A-Z0-9_]*([[:space:]]|$).*\(required\)' "$1" \
      | grep -oE '^#[[:space:]]+[A-Z][A-Z0-9_]*' | grep -oE '[A-Z][A-Z0-9_]*' || true
  } | sort -u
}

provided_env_of() {
  local body
  body="$(grep -vE '^[[:space:]]*#' "$1" || true)"
  grep -oE '(^|[[:space:];&(]|export[[:space:]]+)[A-Z][A-Z0-9_]*=' <<<"$body" \
    | grep -oE '[A-Z][A-Z0-9_]*' | sort -u
}

# Basenames survive being spelled through $SCRIPT_DIR or $ROOT.
scripts_run_by() {
  local body
  body="$(grep -vE '^[[:space:]]*#' "$1" || true)"
  grep -oE '[a-z0-9][a-z0-9-]*\.sh' <<<"$body" | sort -u || true
}

declare -A SCRIPT_BY_BASE=()
while IFS= read -r tracked; do
  SCRIPT_BY_BASE["$(basename "$tracked")"]="$tracked"
done < <(git -C "$REPO_ROOT" ls-files '*.sh')

required_env_closure() {
  local rel="$1" seen="${2:-}" path provided child childrel
  case " $seen " in *" $rel "*) return 0 ;; esac
  path="$REPO_ROOT/$rel"
  [ -f "$path" ] || return 0
  required_env_of "$path"
  provided="$(provided_env_of "$path")"
  while IFS= read -r child; do
    [ -n "$child" ] || continue
    childrel="${SCRIPT_BY_BASE[$child]:-}"
    [ -n "$childrel" ] && [ "$childrel" != "$rel" ] || continue
    required_env_closure "$childrel" "$seen $rel" \
      | { grep -vxF -e "$provided" || true; }
  done < <(scripts_run_by "$path")
}

workflow_env_of() { awk '/^jobs:[[:space:]]*$/ { exit } { print }' "$1"; }

job_names_of() {
  awk '
    /^jobs:[[:space:]]*$/ { injobs = 1; next }
    injobs && /^  [A-Za-z0-9_-]+:[[:space:]]*$/ { n = $1; sub(/:$/, "", n); print n }
  ' "$1"
}

job_block_of() {
  awk -v want="$2" '
    /^jobs:[[:space:]]*$/ { injobs = 1; next }
    !injobs { next }
    /^  [A-Za-z0-9_-]+:[[:space:]]*$/ { n = $1; sub(/:$/, "", n); inwant = (n == want) }
    inwant { print }
  ' "$1"
}

# The demonstration comes first, so a sweep that reproduces nothing fails.
cat >"$env_demo/inner.sh" <<'DEMO'
#!/usr/bin/env bash
set -euo pipefail
run --env "URL=${DEMO_ONLY_NEEDED:?DEMO_ONLY_NEEDED must be set}"
DEMO
cat >"$env_demo/wrapper.sh" <<'DEMO'
#!/usr/bin/env bash
set -euo pipefail
"$ROOT/scripts/inner.sh" one two
DEMO
cat >"$env_demo/flow.yml" <<'DEMO'
name: demo
env:
  UNRELATED: yes
jobs:
  names-it:
    steps:
      - run: scripts/wrapper.sh
        env:
          DEMO_ONLY_NEEDED: set-here
  does-not:
    steps:
      - run: scripts/wrapper.sh
DEMO

required_env_lineform() {
  grep -oE '^: *"\$\{[A-Z][A-Z0-9_]*:\?' "$1" | grep -oE '[A-Z][A-Z0-9_]*' || true
}

if [ -z "$(required_env_lineform "$env_demo/inner.sh")" ]; then
  pass "a refusal written where the value is used is invisible to a line-form reader"
else
  fail "the inline-refusal direction no longer reproduces"
fi
if [ "$(required_env_of "$env_demo/inner.sh")" = "DEMO_ONLY_NEEDED" ]; then
  pass "and is read by this sweep"
else
  fail "this sweep does not read a refusal written where the value is used"
fi

if [ -z "$(required_env_of "$env_demo/wrapper.sh")" ]; then
  pass "a wrapper requires nothing of its own, so the named file answers nothing"
else
  fail "the wrapper direction no longer reproduces"
fi

demo_scope_pooled="$(workflow_env_of "$env_demo/flow.yml")
$(job_block_of "$env_demo/flow.yml" names-it)
$(job_block_of "$env_demo/flow.yml" does-not)"
demo_scope_job="$(workflow_env_of "$env_demo/flow.yml")
$(job_block_of "$env_demo/flow.yml" does-not)"
if grep -qE '(^|[^A-Z0-9_])DEMO_ONLY_NEEDED[=:]' <<<"$demo_scope_pooled" \
  && ! grep -qE '(^|[^A-Z0-9_])DEMO_ONLY_NEEDED[=:]' <<<"$demo_scope_job"; then
  pass "a scope pooling every calling job lets one answer for another; one job's scope does not"
else
  fail "the pooled-scope direction no longer reproduces"
fi

# The membership test is a here-string: a piped `grep -q` loses the race on a large file.
env_bad=""
env_calls=0
while IFS= read -r script; do
  case "$script" in scripts/tests/*) continue ;; esac
  vars="$(required_env_closure "$script" | sort -u)"
  [ -n "$vars" ] || continue
  for wf in "$WORKFLOWS"/*.yml; do
    wf_body="$(grep -vE '^[[:space:]]*#' "$wf" || true)"
    grep -qF "$script" <<<"$wf_body" || continue
    wf_env="$(workflow_env_of "$wf")"
    while IFS= read -r job; do
      [ -n "$job" ] || continue
      block="$(job_block_of "$wf" "$job")"
      block_body="$(grep -vE '^[[:space:]]*#' <<<"$block" || true)"
      grep -qF "$script" <<<"$block_body" || continue
      env_calls=$((env_calls + 1))
      scope="$wf_env"$'\n'"$block"
      for var in $vars; do
        grep -qE "(^|[^A-Z0-9_])${var}[=:]" <<<"$scope" && continue
        env_bad="$env_bad"$'\n'"      $(basename "$wf") job '$job' calls $script without $var"
      done
    done < <(job_names_of "$wf")
  done
done < <(git -C "$REPO_ROOT" ls-files '*.sh')

if [ "$env_calls" -eq 0 ]; then
  fail "the required-input sweep reached no call at all, so it is asserting an absence it never tested"
elif [ -z "$env_bad" ]; then
  pass "each of $env_calls workflow jobs names every input the process it starts refuses to run without"
else
  fail "a script refuses to run without an input its caller never names:$env_bad"
fi

rm -rf "$env_demo"
trap - EXIT
# GitHub runs `run:` blocks under `bash -e`, so a failing command ends the step before `rc=$?`.
demo_captured=$(
  bash -e -c '
    reached=no
    false
    rc=$?
    reached=yes
    echo "rc=$rc reached=$reached"
  ' 2>/dev/null || true
)
demo_guarded=$(
  bash -e -c '
    set +e
    false
    rc=$?
    set -e
    echo "rc=$rc reached=yes"
  ' 2>/dev/null || true
)
if [ -z "$demo_captured" ] && [ "$demo_guarded" = "rc=1 reached=yes" ]; then
  pass "a status read under errexit is unreachable, and a guarded one is not"
else
  fail "the errexit demonstration no longer reproduces (unguarded='$demo_captured' guarded='$demo_guarded')"
fi

# A block that reads `$?` turns errexit off first or takes the status with `|| var=$?`.
status_blocks=0
status_bad=""
while IFS= read -r finding; do
  case "$finding" in
    COUNT*) status_blocks=$((status_blocks + ${finding#COUNT })) ;;
    *) status_bad="$status_bad"$'\n'"      $finding" ;;
  esac
done < <(
  for wf in "$WORKFLOWS"/*.yml; do
    awk -v file="$(basename "$wf")" '
      # A run: block starts at "run:" and holds every line indented past it.
      /^[[:space:]]*(-[[:space:]]+)?run:/ {
        match($0, /^[[:space:]]*/)
        indent = RLENGTH
        inblock = 1; guarded = 0; captures = 0; start = NR
        next
      }
      inblock {
        if ($0 ~ /^[[:space:]]*$/) next
        match($0, /^[[:space:]]*/)
        if (RLENGTH <= indent) { inblock = 0 }
      }
      inblock {
        if ($0 ~ /set[[:space:]]+\+e/) guarded = 1
        # A status taken on the failing command line survives errexit.
        if ($0 ~ /\|\|[[:space:]]*[A-Za-z_][A-Za-z0-9_]*=\$\?/) next
        if ($0 ~ /^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*=\$\?[[:space:]]*$/) {
          captures++
          if (!guarded) {
            printf "%s line %d reads $? under errexit, so the branch below it is unreachable\n", file, NR
          }
        }
      }
      !inblock && captures > 0 { blocks += 1; captures = 0 }
      { if (!inblock) captures = 0 }
      END { printf "COUNT %d\n", blocks + 0 }
    ' "$wf"
  done
)
if [ "$status_blocks" -eq 0 ]; then
  fail "the errexit status sweep reached no status read at all, so it is asserting an absence it never tested"
elif [ -z "$status_bad" ]; then
  pass "each of $status_blocks workflow steps reading an exit status turns errexit off first"
else
  fail "a workflow step reads an exit status errexit has already acted on:$status_bad"
fi

# Each cluster call goes through the retry helper or carries its own reason for not retrying.
RETRY_SCOPE=(
  ".github/workflows/network-drill.yml"
  ".github/workflows/load-test.yml"
  "scripts/loadtest-k6-incluster.sh"
  "scripts/loadtest-quic-incluster.sh"
  "scripts/loadtest-cleanup.sh"
  "deploy/scripts/pg-app-role-sql.sh"
  "deploy/scripts/loadtest-account-sql.sh"
)

retry_bad=""
retry_calls=0
for relative in "${RETRY_SCOPE[@]}"; do
  file="$REPO_ROOT/$relative"
  [ -f "$file" ] || {
    retry_bad="$retry_bad"$'\n'"      $relative is in the retry sweep's scope and is not there"
    continue
  }
  while IFS=$'\t' read -r line rendered; do
    retry_calls=$((retry_calls + 1))
    grep -qF 'kubectl_retry' <<<"$rendered" && continue
    grep -qF 'Not retried' <<<"$rendered" && continue
    retry_bad="$retry_bad"$'\n'"      $relative:$line is a bare cluster call with no reason beside it"
  done < <(awk '
    # Carry the comment block above each call, so a reason written there is part
    # of what the call is read as. A blank line ends the block and a line of code
    # does not: a reason written above `launch() {` is a reason written about the
    # call inside it, and a sweep that lost it there would ask for the same
    # sentence twice.
    /^[[:space:]]*$/ { comments = ""; next }
    /^[[:space:]]*#/ { comments = comments $0 "\n"; next }
    {
      joined = $0
      start = NR
      while (joined ~ /\\[[:space:]]*$/ && (getline next_line) > 0) {
        sub(/\\[[:space:]]*$/, "", joined)
        joined = joined " " next_line
      }
      if (joined ~ /kubectl[^|&;]*(exec|cp)[[:space:]]/) {
        block = comments joined
        gsub(/\t/, " ", block)
        gsub(/\n/, " ", block)
        printf "%d\t%s\n", start, block
        comments = ""
      }
    }
  ' "$file")
done

if [ "$retry_calls" -eq 0 ]; then
  fail "the cluster-call sweep reached no call at all, so it is asserting an absence it never tested"
elif [ -z "$retry_bad" ]; then
  pass "each of $retry_calls cluster calls in the nightlies is retried or says why it is not"
else
  fail "a cluster call in a nightly is neither retried nor exempt:$retry_bad"
fi

# A job naming an environment waits on GitHub to release it, so only a person's approval earns one.
environments_named() {
  python3 - "$@" <<'PY'
import sys

import yaml

for path in sys.argv[1:]:
    with open(path, encoding="utf-8") as handle:
        workflow = yaml.safe_load(handle) or {}
    for job, body in (workflow.get("jobs") or {}).items():
        if not isinstance(body, dict) or "environment" not in body:
            continue
        named = body["environment"]
        if isinstance(named, dict):
            named = named.get("name", "")
        print(f"{path.rsplit('/', 1)[-1]}\t{job}\t{named}")
PY
}

unapproved_environments() {
  local file job named
  while IFS=$'\t' read -r file job named; do
    case "$named" in
      staging | production) ;;
      *) printf ' [%s:%s names %s]' "$file" "$job" "$named" ;;
    esac
  done <<<"$(environments_named "$@")"
}

env_fixture="$(mktemp -d)"
cat >"$env_fixture/alert.yml" <<'YAML'
jobs:
  alert:
    runs-on: ubuntu-24.04
    environment: observability
  deploy:
    runs-on: ubuntu-24.04
    environment:
      name: production
YAML
if [ -n "$(unapproved_environments "$env_fixture/alert.yml")" ]; then
  pass "an alert job naming an environment with no approver is caught"
else
  fail "the environment sweep passes an alert job naming observability, so it proves nothing"
fi
rm -rf "$env_fixture"

env_named="$(environments_named "$WORKFLOWS"/*.yml)"
env_bad="$(unapproved_environments "$WORKFLOWS"/*.yml)"
if [ -z "$env_named" ]; then
  fail "the environment sweep reached no job naming an environment, so it is asserting an absence it never tested"
elif [ -z "$env_bad" ]; then
  pass "every one of $(wc -l <<<"$env_named") environments a workflow names is one a person approves"
else
  fail "a workflow names an environment no person approves, which GitHub can hold indefinitely:$env_bad"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
