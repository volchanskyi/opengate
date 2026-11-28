#!/usr/bin/env bash
# Lists the timed workflow runs nobody has written up, and records a write-up giving each an
# outcome; until then the project's hooks hold every file change and every commit.
#
# Usage:
#   nightly-review.sh fetch             writes .claude/.markers/nightly.due from one GitHub call
#   nightly-review.sh done <write-up>   records a write-up kept outside the work tree
#
# Exit codes:
#   0  listed, or recorded
#   1  GitHub unreachable, or the write-up refused
#   2  usage
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "nightly-review: not inside a git work tree." >&2
  exit 2
}
MARKERS="$ROOT/.claude/.markers"
DUE="$MARKERS/nightly.due"
REVIEWED="$MARKERS/nightly.reviewed"
mkdir -p "$MARKERS"

fetch() {
  local runs
  if ! runs="$(timeout 20 gh run list --event schedule --limit 200 \
    --json databaseId,workflowName,status,conclusion,createdAt,startedAt 2>/dev/null)"; then
    echo unreachable >"$DUE"
    echo "nightly-review: GitHub could not be reached; every file change stays held until a fetch succeeds." >&2
    exit 1
  fi
  touch "$REVIEWED"
  RUNS="$runs" python3 - "$ROOT/.github/workflows" "$REVIEWED" "$DUE" <<'PYEOF'
import datetime as dt, glob, json, os, sys

import yaml

workflows_dir, reviewed_path, due_path = sys.argv[1:4]
runs = json.loads(os.environ["RUNS"] or "[]")
now = dt.datetime.now(dt.timezone.utc)
# A run starts up to about 10 h 35 min after its cron, so 12 hours on top of the period covers it.
LATE = dt.timedelta(hours=12)


def when(stamp):
    return dt.datetime.fromisoformat(stamp.replace("Z", "+00:00"))


def period(cron):
    _minute, hour, dom, month, dow = cron.split()[:5]
    if dow != "*":
        return dt.timedelta(days=7)
    if dom != "*" or month != "*":
        return dt.timedelta(days=31)
    if hour == "*" or "/" in hour or "," in hour:
        return dt.timedelta(hours=1)
    return dt.timedelta(days=1)


timed = []
for path in sorted(glob.glob(os.path.join(workflows_dir, "*.yml"))):
    with open(path, encoding="utf-8") as fh:
        doc = yaml.safe_load(fh) or {}
    # YAML reads the bare key `on` as true.
    triggers = doc.get("on", doc.get(True)) or {}
    schedule = triggers.get("schedule") if isinstance(triggers, dict) else None
    crons = [entry.get("cron") for entry in schedule or [] if entry.get("cron")]
    if crons:
        timed.append((os.path.basename(path), doc.get("name", ""), min(period(c) for c in crons)))

reviewed_runs, reviewed_missing = {}, set()
with open(reviewed_path, encoding="utf-8") as fh:
    for line in fh:
        parts = line.split()
        if len(parts) == 2 and parts[0].isdigit():
            reviewed_runs.setdefault(parts[1], set()).add(int(parts[0]))
        elif len(parts) == 3 and parts[1] == "missing":
            reviewed_missing.add((parts[0], parts[2]))

due = []
for workflow, name, every in timed:
    own = sorted((r for r in runs if r.get("workflowName") == name), key=lambda r: r["databaseId"])
    finished = [r for r in own if r.get("status") == "completed"]
    seen = reviewed_runs.get(workflow)
    # The first review of a workflow starts at its newest finished run.
    fresh = [r for r in finished if r["databaseId"] > max(seen)] if seen else finished[-1:]
    for r in fresh:
        due.append(f'{r["databaseId"]} {workflow} {r.get("conclusion") or "none"} {r["createdAt"]}')
    newest = own[-1] if own else None
    if newest is None or now - when(newest["createdAt"]) > every + LATE:
        last_id = str(newest["databaseId"]) if newest else "none"
        last_at = newest["createdAt"] if newest else "never"
        if (workflow, last_id) not in reviewed_missing:
            due.append(f"{workflow} missing {last_id} {last_at}")

with open(due_path, "w", encoding="utf-8") as fh:
    fh.write("".join(item + "\n" for item in due))
print(f"nightly-review: {len(timed)} timed workflows read, {len(due)} item(s) due")
for item in due:
    print(f"  {item}")
PYEOF
}

record() {
  local writeup="$1" abs
  if [ ! -f "$writeup" ]; then
    echo "nightly-review: no write-up at '$writeup'." >&2
    exit 1
  fi
  abs="$(cd "$(dirname "$writeup")" && pwd)/$(basename "$writeup")"
  case "$abs" in
    "$ROOT"/*)
      echo "nightly-review: the write-up is inside the work tree, where it is content no proof saw; keep it under ~/.cache/opengate/nightly-review/." >&2
      exit 1
      ;;
  esac
  if [ ! -f "$DUE" ] || grep -qx unreachable "$DUE"; then
    echo "nightly-review: there is no due list to answer; run scripts/nightly-review.sh fetch first." >&2
    exit 1
  fi
  python3 - "$DUE" "$writeup" "$REVIEWED" <<'PYEOF'
import re, sys

due_path, writeup_path, reviewed_path = sys.argv[1:4]
with open(due_path, encoding="utf-8") as fh:
    due = [line.split() for line in fh if line.strip()]
with open(writeup_path, encoding="utf-8") as fh:
    lines = fh.read().splitlines()
OUTCOME = r" (regression|broken-test|did-its-job|not-found): \S"
unanswered = [" ".join(item) for item in due
              if not any(re.match(re.escape(item[0]) + OUTCOME, line) for line in lines)]
if unanswered:
    print("nightly-review: the write-up gives these items no line of the form "
          "'<run id | workflow file> <regression | broken-test | did-its-job | not-found>: "
          "<evidence>':", file=sys.stderr)
    for item in unanswered:
        print(f"  {item}", file=sys.stderr)
    sys.exit(1)
with open(reviewed_path, "a", encoding="utf-8") as fh:
    for item in due:
        if item[1] == "missing":
            fh.write(f"{item[0]} missing {item[2]}\n")
        else:
            fh.write(f"{item[0]} {item[1]}\n")
open(due_path, "w", encoding="utf-8").close()
print(f"nightly-review: {len(due)} item(s) recorded; nothing is due")
PYEOF
}

case "${1:-}" in
  fetch) fetch ;;
  "done")
    if [ "$#" -ne 2 ]; then
      echo "usage: scripts/nightly-review.sh done <write-up>" >&2
      exit 2
    fi
    record "$2"
    ;;
  *)
    echo "usage: scripts/nightly-review.sh fetch | done <write-up>" >&2
    exit 2
    ;;
esac
