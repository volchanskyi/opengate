#!/usr/bin/env bash
# Holds the workflows to .claude/rules/ci-cd-determinism.md: a CI/CD step whose
# work was refused must not report success.
#
# Bug history: the deploy's cache token carries read scope only. Two saves were
# refused on every run for two months — the deploy-state entry the pre-flight
# skip stood on, and a toolchain cache added later that never once wrote — and
# both steps were green. A 45.8% skip rate went to zero and nothing said so.
#
# Run: ./scripts/tests/ci-cd-determinism.test.sh
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

# --- the rule is written down and indexed -----------------------------------
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

# --- the deploy asks for no cache at all ------------------------------------
#
# Its token cannot write. Every cache it declares is a save that will be refused
# and reported as a success, so it declares none — not an explicit cache action,
# not a toolchain cache, and not the cache half of a setup action.
CD="$WORKFLOWS/cd.yml"
for shape in 'actions/cache' 'Swatinem/rust-cache' 'cache-to:' '^[[:space:]]*cache:[[:space:]]'; do
  if grep -qE -- "$shape" "$CD"; then
    fail "cd.yml declares a cache ($shape); its token cannot write, so that save is refused and reported as a success"
  else
    pass "cd.yml declares no cache ($shape)"
  fi
done

# --- every cache write we name is read back ---------------------------------
#
# An inline save names its own key, so nothing stops the same key being asserted
# in the step after it. A save with no read-back is the defect this rule is about.
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

# --- the agent build's cache is read back -----------------------------------
#
# The deploy no longer builds the agent; the image workflow does, and it is that
# workflow's cache keeping the cross-build off a cold start. A refusal there
# would cost three minutes on every run with nothing saying why.
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

# --- an artifact nobody wrote is not an artifact ------------------------------
#
# Every artifact mutation.yml uploads is an input to the aggregation that scores
# the night. A shard whose tool wrote no report uploads nothing, and the upload
# action's default is to say so in a warning and exit zero — so the shard job is
# green, and the only symptom is the publish job reporting an incomplete set
# without naming what went missing or why. The shard is where the fact is known,
# so the shard is where it has to fail.
MUTATION="$WORKFLOWS/mutation.yml"
uploads="$(grep -c 'uses: actions/upload-artifact' "$MUTATION" || true)"
errors="$(grep -c 'if-no-files-found: error' "$MUTATION" || true)"
if [ "$uploads" -gt 0 ] && [ "$uploads" -eq "$errors" ]; then
  pass "all $uploads mutation.yml artifact uploads fail on an empty file set"
else
  fail "mutation.yml has $uploads artifact upload(s) but only $errors fail on an empty set — a shard that produced no report would report success"
fi

# --- a tool a workflow builds resolves the graph it was tested against -------
#
# `cargo install <tool>` re-resolves that tool's whole dependency graph to
# "latest compatible versions" on every run, so a workflow that installs one is
# building software nobody has ever built before. It does not fail as a
# vulnerability or a version bump; it fails as a compile error deep inside a
# transitive crate, in a job whose subject is something else entirely — and it
# is invisible on a workstation, where the tool was installed once and is never
# rebuilt. `--locked` uses the lockfile the tool's own authors tested, which is
# the only build anybody has evidence about.
install_bad=""
while IFS= read -r line; do
  # Prose in a comment is not an install.
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

# --- a read is spelled as a read ---------------------------------------------
#
# `gh api` chooses its own HTTP method: GET normally, and POST the moment any
# field flag is present. So a read that narrows its result with `-f`/`-F` is
# silently posted, and every list endpoint answers a POST with `404 Not Found` —
# which reads as "the workflow does not exist" rather than "you asked wrongly".
# Under `set -euo pipefail` the step then dies on a message about the wrong
# thing, and the shapes around it are built to treat an absent run as a reason
# to stand down quietly.
#
# The nightly drill lost a whole night's measurement to this: its search for the
# image build that carries the agent binary was a POST, the 404 killed the step,
# and the run reported no machine to measure. So every `gh api` that passes a
# field flag states its method rather than letting the tool infer one.
#
# This file is the one place excluded, because it necessarily carries the
# pattern it matches: its own matcher strings and the prose above are not calls.
# Counting them would let the sweep satisfy its own tripwire by reading itself,
# which is the absence-shaped check this rule warns about.
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
  # Join backslash continuations so a wrapped invocation is judged whole.
  while IFS= read -r entry; do
    where="${entry%%$'\t'*}"
    cmd="${entry#*$'\t'}"
    case "$cmd" in
      *'gh api'*) ;;
      *) continue ;;
    esac
    # Prose about the tool is not a call to it.
    case "$cmd" in
      '#'*) continue ;;
    esac
    gh_seen=$((gh_seen + 1))
    # A field flag is what flips the inferred method to POST.
    printf '%s' "$cmd" \
      | grep -qE '(^|[[:space:]])(-f|-F|--field|--raw-field)([[:space:]=])' || continue
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

# --- an input a script refuses to run without is named where it is called -----
#
# A script that documents an input as required, or refuses to start without one,
# holds a contract with every workflow that calls it, and nothing was reading
# that contract. `scripts/loadtest-quic-incluster.sh` works through the pod named
# in LOADTEST_POD. The nightly drill calls it holding its pod in FLEET_POD — a
# name the shim has never heard of — so the call was refused where it stood: no
# fleet started, the drill measured nothing, and the whole of it surfaced two
# hours into a nightly instead of in the commit that wrote the call.
#
# This is the same shape as the `gh api` verb above. There, a tool inferred a
# verb nobody wrote down; here, a caller assumed a name nobody checked. Both are
# decisions made at a distance from the text that carries them, and both come
# back as an error about something else.
#
# The name has to be reachable from the call, so the scope searched is the
# calling job plus the workflow-level env every job inherits. $GITHUB_ENV does
# not cross a job boundary and neither does this check.
required_env_of() {
  {
    # The refusal the shell itself makes: `: "${VAR:?…}"`.
    grep -oE '^: *"\$\{[A-Z][A-Z0-9_]*:\?' "$1" | grep -oE '[A-Z][A-Z0-9_]*' || true
    # The script's own account of itself, in its Environment header.
    grep -E '^#[[:space:]]+[A-Z][A-Z0-9_]*([[:space:]]|$).*\(required\)' "$1" \
      | grep -oE '^#[[:space:]]+[A-Z][A-Z0-9_]*' | grep -oE '[A-Z][A-Z0-9_]*' || true
  } | sort -u
}

# The calling job's text, with the workflow-level env prepended.
calling_scope() {
  awk -v want="$2" '
    /^jobs:[[:space:]]*$/ { injobs = 1; next }
    !injobs { pre = pre $0 "\n"; next }
    /^  [A-Za-z0-9_-]+:[[:space:]]*$/ {
      if (block != "" && index(block, want)) out = out block
      block = ""
    }
    { block = block $0 "\n" }
    END {
      if (block != "" && index(block, want)) out = out block
      printf "%s%s", pre, out
    }
  ' "$1"
}

env_bad=""
env_calls=0
while IFS= read -r script; do
  case "$script" in scripts/tests/*) continue ;; esac
  vars="$(required_env_of "$REPO_ROOT/$script" || true)"
  [ -n "$vars" ] || continue
  for wf in "$WORKFLOWS"/*.yml; do
    # A mention in a comment is not a call.
    grep -vE '^[[:space:]]*#' "$wf" | grep -qF "$script" || continue
    env_calls=$((env_calls + 1))
    scope="$(calling_scope "$wf" "$script")"
    for var in $vars; do
      grep -qE "(^|[^A-Z0-9_])${var}[=:]" <<<"$scope" && continue
      env_bad="$env_bad"$'\n'"      $(basename "$wf") calls $script without $var"
    done
  done
done < <(git -C "$REPO_ROOT" ls-files '*.sh')

if [ "$env_calls" -eq 0 ]; then
  fail "the required-input sweep reached no call at all, so it is asserting an absence it never tested"
elif [ -z "$env_bad" ]; then
  pass "each of $env_calls workflow calls names every input its script refuses to run without"
else
  fail "a script refuses to run without an input its caller never names:$env_bad"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
