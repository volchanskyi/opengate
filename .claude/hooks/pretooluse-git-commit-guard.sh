#!/usr/bin/env bash
# Refuses a commit in this project's clones and work trees at the first failing check: trailer,
# skipped hooks, identity, branch, upstream, nightly review, Plan: line, proofs, TDD backup.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
# shellcheck source=lib/tidy-up.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/tidy-up.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"
[ -n "$cmd" ] || exit 0

if ! grep -qE "$(git_verb_re commit)" <<<"$cmd"; then
  exit 0
fi

if belongs_to_other_project "$cmd"; then
  exit 0
fi

if grep -qiF 'Co-Authored-By' <<<"$cmd"; then
  block git-no-co-authored-by "git commit refused: message contains Co-Authored-By. .claude/rules/git.md requires no trailers. Remove it and re-issue."
fi

# Python reads the commit's own options the way git does: a short cluster stops at the option that
# takes a value, so `-nm x` skips the hooks and `-mn` is the message "n".
commit_args="$(
  CMD="$cmd" python3 - <<'PYEOF'
import json, os, shlex

cmd = os.environ["CMD"]
lex = shlex.shlex(cmd.replace("\n", " ; "), posix=True, punctuation_chars=";&|")
lex.whitespace_split = True
try:
    tokens = list(lex)
except ValueError:
    tokens = cmd.split()
segments, current = [], []
for token in tokens:
    if token and set(token) <= set(";&|"):
        segments.append(current)
        current = []
    else:
        current.append(token)
segments.append(current)

GIT_VALUE_OPTS = {"-c", "-C", "--git-dir", "--work-tree", "--namespace", "--exec-path"}
SHORT_VALUE = set("mFCct")
SHORT_OPTIONAL = set("Su")
LONG_VALUE = {"--author", "--date", "--cleanup", "--fixup", "--squash", "--trailer",
              "--reuse-message", "--reedit-message", "--template", "--pathspec-from-file"}
found = {"no_verify": False, "messages": [], "file": None}
for seg in segments:
    if "git" not in seg:
        continue
    i = seg.index("git") + 1
    while i < len(seg) and seg[i].startswith("-"):
        i += 2 if seg[i] in GIT_VALUE_OPTS else 1
    if i >= len(seg) or seg[i] != "commit":
        continue
    args, j = seg[i + 1:], 0
    while j < len(args):
        arg = args[j]
        if arg == "--":
            break
        if arg == "--no-verify":
            found["no_verify"] = True
        elif arg.startswith("--message=") or arg.startswith("--file="):
            key, value = arg.split("=", 1)
            if key == "--message":
                found["messages"].append(value)
            else:
                found["file"] = value
        elif arg in ("--message", "--file") and j + 1 < len(args):
            if arg == "--message":
                found["messages"].append(args[j + 1])
            else:
                found["file"] = args[j + 1]
            j += 1
        elif arg in LONG_VALUE:
            j += 1
        elif arg.startswith("-") and not arg.startswith("--"):
            for k, letter in enumerate(arg[1:]):
                if letter == "n":
                    found["no_verify"] = True
                elif letter in SHORT_VALUE:
                    value = arg[k + 2:]
                    if not value and j + 1 < len(args):
                        value = args[j + 1]
                        j += 1
                    if letter == "m":
                        found["messages"].append(value)
                    elif letter == "F":
                        found["file"] = value
                    break
                elif letter in SHORT_OPTIONAL:
                    break
        j += 1
print(json.dumps(found))
PYEOF
)"

skips_hooks="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["no_verify"])' "$commit_args")"
if [ "$skips_hooks" = "True" ]; then
  block git-no-verify "git commit refused: --no-verify (or -n) skips git's own check of the commit. Fix the underlying hook failure or remove the flag."
fi

# Git's own pre-commit check lives in the hooks folder, so moving the folder skips it.
if grep -qiE 'core\.hookspath|GIT_CONFIG_PARAMETERS|GIT_CONFIG_COUNT' <<<"$cmd" \
  || grep -qiF 'core.hookspath' <<<"${GIT_CONFIG_PARAMETERS:-}"; then
  block git-hooks-path "git commit refused: the command moves git's hooks folder (core.hooksPath, GIT_CONFIG_PARAMETERS or GIT_CONFIG_COUNT), which skips git's own check of the commit. Remove the override."
fi

# An identity override on the command line wins over the repo config.
override_email="$(printf '%s' "$cmd" | grep -oE -- '-c[[:space:]]+user\.email=[^[:space:]]+' | tail -1 | sed -E 's/^-c[[:space:]]+user\.email=//' || true)"
override_name="$(printf '%s' "$cmd" | grep -oE -- '-c[[:space:]]+user\.name=[^[:space:]]+' | tail -1 | sed -E 's/^-c[[:space:]]+user\.name=//' || true)"
eff_email="${override_email:-$(git config user.email 2>/dev/null || echo "")}"
eff_name="${override_name:-$(git config user.name 2>/dev/null || echo "")}"

eff_email="${eff_email%\"}"
eff_email="${eff_email#\"}"
eff_email="${eff_email%\'}"
eff_email="${eff_email#\'}"
eff_name="${eff_name%\"}"
eff_name="${eff_name#\"}"
eff_name="${eff_name%\'}"
eff_name="${eff_name#\'}"

if [ "$eff_email" != "ivan.volchanskyi@gmail.com" ] || [ "$eff_name" != "Ivan Volchanskyi" ]; then
  block git-identity "git commit refused: identity is '$eff_name <$eff_email>'. .claude/rules/git.md requires Ivan Volchanskyi <ivan.volchanskyi@gmail.com>. Fix with: git config user.name \"Ivan Volchanskyi\" && git config user.email \"ivan.volchanskyi@gmail.com\"."
fi

current_branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
if [ "$current_branch" = "main" ]; then
  block git-branch-not-main "git commit refused: HEAD is on main. .claude/rules/git.md: main receives code only via the auto-merge CI job. Switch to dev."
fi

# The upstream check is skipped when the fetch fails offline.
if git fetch --quiet origin dev 2>/dev/null; then
  if git rev-parse --verify --quiet origin/dev >/dev/null 2>&1; then
    behind="$(git rev-list --count HEAD..origin/dev 2>/dev/null || echo 0)"
    if [ "${behind:-0}" -gt 0 ] 2>/dev/null; then
      summary="$(git log --format='%h %s' HEAD..origin/dev 2>/dev/null | head -5 || true)"
      block git-behind-upstream "git commit refused: local HEAD is $behind commit(s) behind origin/dev. Run: git pull --rebase=merges origin dev. Upstream commits:
$summary"
    fi
  fi
fi

if ! nightly_review_done; then
  block nightly-review "git commit refused: the nightly review is not done. $(nightly_hold_reason) .claude/rules/nightly-review.md."
fi

root="$(project_root)"

# Prints each active plan's path, one a line.
active_plans() {
  local plan
  for plan in "$root"/.claude/plans/*.md; do
    if [ -f "$plan" ] && [ "$(plan_status "$plan")" = "active" ]; then
      printf '%s\n' "${plan#"$root"/}"
    fi
  done
  return 0
}

# The fast lane is open while the newest deploy failed or production's health address, the one the
# deploy's edge smoke test reaches, answers anything but 200.
fast_lane_open() {
  local conclusion base status
  conclusion="$(timeout 15 gh run list --workflow cd.yml --limit 1 --json conclusion --jq '.[0].conclusion' 2>/dev/null || true)"
  [ "$conclusion" != "failure" ] || return 0
  base="$(sed -nE '/^[[:space:]]*baseURL:/{s/^[[:space:]]*baseURL:[[:space:]]*"?([^"[:space:]]+)"?.*/\1/p;q}' \
    "$root/deploy/helm/opengate/values-production.yaml" 2>/dev/null || true)"
  [ -n "$base" ] || return 1
  status="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$base/api/v1/health" 2>/dev/null || true)"
  [ "$status" != "200" ]
}

message="$(
  python3 - "$commit_args" <<'PYEOF'
import json, sys

found = json.loads(sys.argv[1])
if found["file"] not in (None, "", "-"):
    try:
        with open(found["file"], encoding="utf-8", errors="replace") as fh:
            print(fh.read())
    except OSError:
        pass
elif found["messages"]:
    print("\n\n".join(found["messages"]))
PYEOF
)"
plan_line="$(awk 'NF { last = $0 } END { print last }' <<<"$message")"
plan_help="The message's last line is one of: 'Plan: .claude/plans/<name>.md' (an active plan, or one this commit removes while adding its phases.md row), 'Plan: none' (no plan is active), 'Plan: fast lane' (the newest deploy failed or production is down). Write the message to a file outside the work tree and pass it with -F. .claude/rules/plans-and-adrs.md."
case "$plan_line" in
  "Plan: none")
    active="$(active_plans)"
    if [ -n "$active" ]; then
      block git-plan-line "git commit refused: the message says 'Plan: none' while these plans are active: $(tr '\n' ' ' <<<"$active")— a fix found along the way rides the active plan. $plan_help"
    fi
    ;;
  "Plan: fast lane")
    if ! fast_lane_open; then
      block git-plan-line "git commit refused: 'Plan: fast lane' is for production down or deploys failing, and the newest cd.yml run did not fail while production's health address answers 200. $plan_help"
    fi
    ;;
  "Plan: .claude/plans/"*.md)
    named="${plan_line#Plan: }"
    if [ -f "$root/$named" ]; then
      if [ "$(plan_status "$root/$named")" != "active" ]; then
        block git-plan-line "git commit refused: $named is not active (its status line reads '$(plan_status "$root/$named")'). A plan is set active only with nothing open in it. $plan_help"
      fi
    else
      staged_phases="$(git diff --cached -U0 -- .claude/phases.md 2>/dev/null || true)"
      if ! grep -qE '^\+\|' <<<"$staged_phases"; then
        block git-plan-line "git commit refused: $named is not on disk and the commit adds no .claude/phases.md row, as the commit landing a plan does. $plan_help"
      fi
    fi
    ;;
  *)
    block git-plan-line "git commit refused: the message has no Plan: line (its last line reads '${plan_line}'). $plan_help"
    ;;
esac

if ! tidy_done_matches; then
  block git-tidy-up-first "git commit refused: no finished /refactor for the content on disk. The order is: /refactor (scripts/refactor-gate.sh start … finish), then ./scripts/precommit-gauntlet.sh, then the commit. Any edit after /refactor finishes — a new file included — starts the order over. Keep the commit message file, and any log of the run, outside the work tree: a new file there is content the proofs never saw."
fi

if ! gauntlet_pass_matches; then
  block git-gauntlet-pass "git commit refused: no gauntlet pass for the content on disk from the last hour. Run ./scripts/precommit-gauntlet.sh (in the background, no time limit); a full pass over unchanged content writes .claude/.markers/gauntlet.pass, good for $((GAUNTLET_PASS_SECONDS / 60)) minutes from the run's start. A failed or cut-short run writes nothing."
fi

if ! "$TDD_CHECK" has-test-change; then
  base="$(git merge-base HEAD origin/dev 2>/dev/null \
    || git merge-base HEAD dev 2>/dev/null \
    || git merge-base HEAD origin/main 2>/dev/null \
    || git merge-base HEAD main 2>/dev/null \
    || git rev-list --max-parents=0 HEAD 2>/dev/null | head -1 \
    || echo HEAD)"
  branch_files="$({
    git diff --name-only "$base"..HEAD 2>/dev/null || true
    git diff --cached --name-only 2>/dev/null || true
    git diff --name-only 2>/dev/null || true
  } | sort -u | grep -v '^$' || true)"
  has_source=false
  if [ -n "$branch_files" ]; then
    while IFS= read -r f; do
      [ -n "$f" ] || continue
      if "$TDD_CHECK" is-source "$f"; then
        has_source=true
        break
      fi
    done <<<"$branch_files"
  fi
  if [ "$has_source" = "true" ]; then
    block tdd-backup-check "git commit refused (TDD backup): branch has source changes but no test changes. Stage at least one test file before committing. There is NO bypass."
  fi
fi

exit 0
