#!/usr/bin/env bash
# Holds the workflows to .claude/rules/workflow-inputs.md: what a workflow hands a script reaches it
# named in the calling job, as a value, by the method the call states.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   [%s] %s\n' "$1" "$2"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("[$1] $2")
  printf '  FAIL [%s] %s\n' "$1" "$2" >&2
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Reads every file once and prints read<TAB>sweep<TAB>N and finding<TAB>sweep<TAB>text.
cat >"$WORK/sweeps.py" <<'PY'
import os
import re
import subprocess
import sys

import yaml

ROOT = sys.argv[1]
SELF = "scripts/tests/workflow-inputs.test.sh"
COMMENT = re.compile(r"^[ \t]*#")
INLINE = re.compile(r"\$\{([A-Z][A-Z0-9_]*):\?")
HEADER = re.compile(r"^#[ \t\r\f\v]+([A-Z][A-Z0-9_]*)[ \t\r\f\v].*\(required\)", re.M)
PROVIDES = re.compile(r"(?:^|[ \t\r\f\v;&(]|export[ \t\r\f\v]+)([A-Z][A-Z0-9_]*)=", re.M)
ASSIGNS = re.compile(r"(?:^|[^A-Z0-9_])([A-Z][A-Z0-9_]*)=", re.M)
PRINT = re.compile(r"^[ \t]*(printf|echo)([ \t]|$)")
SCRIPT = re.compile(r"[a-z0-9][a-z0-9-]*\.sh")
FIELD = re.compile(r"(^|[ \t])(-f|-F|--field|--raw-field)([ \t=])")
PASTE = re.compile(r"\$\{\{[^}]*?\b(inputs\.[\w-]+|github\.event\.[\w.-]+|github\.head_ref)\b[^}]*\}\}")


def listed(*patterns):
    if os.path.isdir(os.path.join(ROOT, ".git")):
        out = subprocess.run(["git", "-C", ROOT, "ls-files", *patterns], capture_output=True,
                             text=True, check=True).stdout
        return sorted(set(out.split()))
    found = []
    for base, _, files in os.walk(ROOT):
        for name in files:
            rel = os.path.relpath(os.path.join(base, name), ROOT)
            if any(re.fullmatch(p.replace(".", r"\.").replace("*", ".*"), rel) for p in patterns):
                found.append(rel)
    return sorted(found)


TEXT = {}


def text_of(rel):
    if rel not in TEXT:
        path = os.path.join(ROOT, rel)
        TEXT[rel] = open(path, encoding="utf-8").read() if os.path.isfile(path) else None
    return TEXT[rel]


def code(text):
    return "\n".join(line for line in text.split("\n") if not COMMENT.match(line))


def joined(text):
    lines, buf = [], ""
    for line in text.split("\n"):
        buf += line
        if buf.endswith("\\"):
            buf = buf[:-1]
            continue
        lines.append(buf)
        buf = ""
    return lines + ([buf] if buf else [])


def runs(text):
    calls = [line for line in joined(code(text)) if not PRINT.match(line)]
    return set(SCRIPT.findall("\n".join(calls)))


SCRIPTS = listed("*.sh")
BY_BASE = {os.path.basename(path): path for path in SCRIPTS}


def closure(rel, seen=frozenset()):
    text = text_of(rel)
    if rel in seen or text is None:
        return set()
    need = set(INLINE.findall(code(text))) | set(HEADER.findall(text))
    provided = set(PROVIDES.findall(code(text)))
    for child in runs(text):
        path = BY_BASE.get(child)
        if path and path != rel:
            need |= closure(path, seen | {rel}) - provided
    return need


def strings(node):
    if isinstance(node, dict):
        for value in node.values():
            yield from strings(value)
    elif isinstance(node, list):
        for value in node:
            yield from strings(value)
    elif isinstance(node, str):
        yield node


def job_scope(workflow, job):
    names = set((workflow.get("env") or {}).keys()) | set((job.get("env") or {}).keys())
    body = [code(s) for s in strings({k: v for k, v in job.items() if k not in ("steps", "env")})]
    for step in job.get("steps") or []:
        names |= set((step.get("env") or {}).keys())
        body += [code(s) for s in strings({k: v for k, v in step.items() if k != "env"})]
        if isinstance(step.get("run"), str):
            names |= set(ASSIGNS.findall(code(step["run"])))
    return "\n".join(body), names


def required_inputs(workflows):
    calls = 0
    for name, text, workflow in workflows:
        named = [s for s in SCRIPTS if not s.startswith("scripts/tests/") and s in code(text)]
        for job_name, job in (workflow.get("jobs") or {}).items():
            body, names = job_scope(workflow, job)
            for script in named:
                need = closure(script)
                if not need or script not in body:
                    continue
                calls += 1
                for var in sorted(need - names):
                    print(f"finding\trequired-input\t{name} job '{job_name}' calls {script} without {var}")
    print(f"read\trequired-input\t{calls}")


def pasted_inputs(documents):
    blocks = 0
    for name, document in documents:
        jobs = [(job, body.get("steps") or []) for job, body in (document.get("jobs") or {}).items()]
        jobs.append(("runs", (document.get("runs") or {}).get("steps") or []))
        for job, steps in jobs:
            for index, step in enumerate(steps):
                run = step.get("run") if isinstance(step, dict) else None
                if not isinstance(run, str):
                    continue
                blocks += 1
                for match in PASTE.finditer(run):
                    print(f"finding\tinput-as-value\t{name}: {job}: {step.get('name') or index}: {match.group(1)}")
    print(f"read\tinput-as-value\t{blocks}")


def gh_methods(paths):
    seen = fielded = 0
    for rel in paths:
        text = text_of(rel)
        if rel == SELF or text is None or "gh api" not in text:
            continue
        for number, line in enumerate(joined(text), 1):
            command = line.strip()
            if "gh api" not in command or command.startswith("#"):
                continue
            seen += 1
            if not FIELD.search(command):
                continue
            fielded += 1
            if "-X " not in command and "--method " not in command:
                print(f"finding\tread-spelled\t{rel}:{number}")
    print(f"read\tread-spelled\t{seen} {fielded}")


workflow_paths = listed(".github/workflows/*.yml")
action_paths = listed(".github/actions/*/action.yml")
workflows = [(os.path.basename(p), text_of(p), yaml.safe_load(text_of(p)) or {}) for p in workflow_paths]
required_inputs(workflows)
actions = [(p, yaml.safe_load(text_of(p)) or {}) for p in action_paths]
pasted_inputs([(name, doc) for name, _, doc in workflows] + actions)
gh_methods(workflow_paths + SCRIPTS)
PY

read_of() { awk -F'\t' -v s="$2" '$1 == "read" && $2 == s { print $3 }' <<<"$1"; }
findings_of() { awk -F'\t' -v s="$2" '$1 == "finding" && $2 == s { print "      " $3 }' <<<"$1"; }

echo "workflow-inputs:"

# Each sweep runs over a tree built to hold its defect, beside the shape it accepts.
DEMO="$WORK/demo"
mkdir -p "$DEMO/scripts" "$DEMO/.github/workflows" "$DEMO/.github/actions/paste"
cat >"$DEMO/scripts/inner.sh" <<'DEMO'
#!/usr/bin/env bash
set -euo pipefail
run --env "URL=${DEMO_ONLY_NEEDED:?DEMO_ONLY_NEEDED must be set}"
DEMO
cat >"$DEMO/scripts/wrapper.sh" <<'DEMO'
#!/usr/bin/env bash
set -euo pipefail
"$ROOT/scripts/inner.sh" one two
DEMO
cat >"$DEMO/scripts/lister.sh" <<'DEMO'
#!/usr/bin/env bash
listed() {
  printf '%s\n' \
    scripts/inner.sh \
    deploy/other.sh
}
DEMO
cat >"$DEMO/scripts/caches.sh" <<'DEMO'
#!/usr/bin/env bash
gh api repos/o/r/actions/caches -f key=one
gh api -X GET repos/o/r/actions/caches \
  -f key=two
DEMO
cat >"$DEMO/.github/workflows/flow.yml" <<'DEMO'
name: demo
on: workflow_dispatch
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
  lists-it:
    steps:
      - run: scripts/lister.sh
  pastes:
    steps:
      - name: Determine tag
        run: TAG="${{ inputs.image_tag }}"
      - name: Read the head
        run: echo "${{ github.event.workflow_run.head_sha }} ${{ github.head_ref }}"
  through-env:
    steps:
      - name: Carry the tag
        env:
          TAG: ${{ inputs.image_tag }}
        run: echo "$TAG ${{ github.sha }}"
DEMO
cat >"$DEMO/.github/actions/paste/action.yml" <<'DEMO'
runs:
  using: composite
  steps:
    - name: Paste
      shell: bash
      run: echo "${{ inputs.ref }}"
DEMO
demo="$(python3 "$WORK/sweeps.py" "$DEMO")"
real="$(python3 "$WORK/sweeps.py" "$REPO_ROOT")"

demo_required="$(findings_of "$demo" required-input)"
if [ -z "$(grep -oE '^: *"\$\{[A-Z][A-Z0-9_]*:\?' "$DEMO/scripts/inner.sh" || true)" ] \
  && grep -qF "flow.yml job 'does-not' calls scripts/wrapper.sh without DEMO_ONLY_NEEDED" <<<"$demo_required" \
  && grep -qE '^[[:space:]]+DEMO_ONLY_NEEDED:' "$DEMO/.github/workflows/flow.yml" \
  && ! grep -qF "'names-it'" <<<"$demo_required" \
  && ! grep -qF "'lists-it'" <<<"$demo_required"; then
  pass required-input "a refusal where the value is used, reached through a wrapper, is asked of each calling job alone; a printed path calls nothing"
else
  fail required-input "the required-input sweep does not reproduce its defect: [$demo_required]"
fi
calls="$(read_of "$real" required-input)"
real_required="$(findings_of "$real" required-input)"
if [ "${calls:-0}" -eq 0 ]; then
  fail required-input "the required-input sweep reached no call at all, so it is asserting an absence it never tested"
elif [ -z "$real_required" ]; then
  pass required-input "each of $calls workflow jobs names every input the process it starts refuses to run without"
else
  fail required-input "a script refuses to run without an input its caller never names:"$'\n'"$real_required"
fi

payload="x\"; touch \"$WORK/ran\"; echo \""
# shellcheck disable=SC2016 # the expression is GitHub's to fill in and the variable the script's own
pasted_script='TAG="${{ inputs.image_tag }}"; echo "$TAG"'
bash -c "${pasted_script//'${{ inputs.image_tag }}'/$payload}" >/dev/null 2>&1 || true
pasted_ran="$([ -e "$WORK/ran" ] && echo yes || echo no)"
rm -f "$WORK/ran"
TAG="$payload" bash -c 'echo "$TAG"' >/dev/null 2>&1 || true
env_ran="$([ -e "$WORK/ran" ] && echo yes || echo no)"
demo_paste="$(findings_of "$demo" input-as-value)"
if [ "$pasted_ran" = yes ] && [ "$env_ran" = no ] \
  && grep -qF 'flow.yml: pastes: Determine tag: inputs.image_tag' <<<"$demo_paste" \
  && grep -qF 'flow.yml: pastes: Read the head: github.head_ref' <<<"$demo_paste" \
  && grep -qF 'action.yml: runs: Paste: inputs.ref' <<<"$demo_paste" \
  && ! grep -qF 'through-env' <<<"$demo_paste"; then
  pass input-as-value "a pasted input runs as code and one read through env does not, and the sweep tells them apart"
else
  fail input-as-value "the paste sweep does not reproduce: pasted ran=$pasted_ran, env ran=$env_ran, found [$demo_paste]"
fi
blocks="$(read_of "$real" input-as-value)"
real_paste="$(findings_of "$real" input-as-value)"
if [ "${blocks:-0}" -eq 0 ]; then
  fail input-as-value "the paste sweep read no run: block, so it is asserting an absence it never tested"
elif [ -z "$real_paste" ]; then
  pass input-as-value "none of $blocks run: blocks pastes an input or an event field into its own text"
else
  fail input-as-value "a run: block pastes an input or an event field into its own text; pass it through env:"$'\n'"$real_paste"
fi

demo_gh="$(findings_of "$demo" read-spelled)"
if [ "$(read_of "$demo" read-spelled)" = "2 2" ] && [ "$demo_gh" = "      scripts/caches.sh:2" ]; then
  pass read-spelled "a field flag with no method is caught, and one stating its method across a line break is not"
else
  fail read-spelled "the gh api sweep does not tell an inferred method from a stated one: [$demo_gh]"
fi
read -r seen_calls fielded <<<"$(read_of "$real" read-spelled)"
real_gh="$(findings_of "$real" read-spelled)"
if [ "${seen_calls:-0}" -eq 0 ]; then
  fail read-spelled "the gh api sweep reached no call at all, so it is asserting an absence it never tested"
elif [ -z "$real_gh" ]; then
  pass read-spelled "of $seen_calls gh api calls, the $fielded passing a field flag state their HTTP method"
else
  fail read-spelled "gh api infers POST from a field flag, and a list endpoint answers a POST with 404:"$'\n'"$real_gh"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
