#!/usr/bin/env bash
# Writes a nightly browser run's summary, as Markdown, from Playwright's JSON report.
#
# Usage: e2e-summarize.sh performance|cross-browser <report.json>
#   performance    each reading the speed specs recorded, beside the limit it is held to
#   cross-browser  each browser's passed, failed, retried and skipped tests, and which failed
#
# Exit codes:
#   0  summary written, including for a run that produced nothing to read
#   2  usage
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/summary-table.sh
. "$HERE/lib/summary-table.sh"

usage() {
  echo "usage: $0 performance|cross-browser <report.json>" >&2
  exit 2
}

[ "$#" -eq 2 ] || usage
RUN="$1"
REPORT="$2"
case "$RUN" in
  performance)
    TITLE='E2E Performance'
    PURPOSE="Times typing and downloads over routers shaped to an office line, and the page's memory through a long download, against the limits the product promises."
    ;;
  cross-browser)
    TITLE='E2E Cross-Browser'
    PURPOSE='Runs the browser suite in Chromium, Firefox and WebKit, each also from behind a home router, and retries a failed test once.'
    ;;
  *) usage ;;
esac

nothing() {
  printf '## %s\n\n%s\n\n**Result:** the run produced nothing to read: %s\n' "$TITLE" "$PURPOSE" "$1"
  exit 0
}

[ -f "$REPORT" ] || nothing 'Playwright wrote no report.'
# One object per test and browser: its status, title and path, the first error, and its readings.
tests="$(jq -c '
  def plain: (gsub("\u001b\\[[0-9;]*m"; "") | split("\n")[0]) // "";
  def specs($path): (.specs[]? | {path: ($path + [.title]), title, tests}),
    (.suites[]? as $s | $s | specs($path + [$s.title]));
  [.suites[]? as $f | $f | specs([$f.title]) | . as $spec | .tests[]
    | {project: .projectName, status, title: $spec.title, path: ($spec.path | join(" › ")),
       ended: (.results[-1].status // "not run"),
       error: ([.results[] | .error.message? // empty] | first // "" | plain),
       readings: [.results[-1].annotations[]? | select(.type == "measurement") | .description | fromjson]}]
' "$REPORT" 2>/dev/null)" || nothing 'the report does not parse.'
[ "$(jq 'length' <<<"$tests")" -gt 0 ] || nothing 'the report holds no test.'

printf '## %s\n\n%s\n\n' "$TITLE" "$PURPOSE"

performance() {
  jq -r '
    [.[] | select(.status != "expected" and .status != "flaky")] as $failed
    | if ($failed | length) == 0 then "**Result:** pass: all \(length) speed specs held their limits."
      else "**Result:** FAIL: \($failed | length) of \(length) speed specs failed: \($failed | map(.title) | join("; "))."
      end' <<<"$tests"
  printf '\n'
  summary_table_header
  jq -r '
    def cell: gsub("\n"; " ") | gsub("\\|"; "\\|");
    def word: {passed: "passed", failed: "failed", timedOut: "timed out", interrupted: "interrupted",
               skipped: "skipped"}[.] // .;
    def outcome: if .status == "expected" or .status == "flaky" then "pass" else "FAIL" end;
    .[]
    | (.readings[] as $r
        | "| \(.title | cell) — \($r.measurement | cell) | \($r.limit | cell) | \($r.reading | cell) | \(if $r.held then "pass" else "FAIL" end) |"),
      (if (.readings | length) == 0 or (outcome == "FAIL" and all(.readings[]; .held))
       then "| \(.title | cell) | passes | \(.ended | word)\(if .error == "" then "" else ": " + (.error | cell) end) | \(outcome) |"
       else empty end)' <<<"$tests"
}

cross_browser() {
  jq -r '
    ([.[] | select(.status == "unexpected")] | length) as $failed
    | ([.[] | select(.status == "flaky")] | length) as $retried
    | (if $failed == 0 then "**Result:** pass: none of the \(length) tests failed."
       else "**Result:** FAIL: \($failed) of \(length) tests failed." end)
      + (if $retried == 0 then "" else " \($retried) passed only on retry." end)' <<<"$tests"
  printf '\n| Browser | Passed | Failed | Passed on retry | Skipped |\n|---|---|---|---|---|\n'
  jq -r '
    group_by(.project)[]
    | def count($s): [.[] | select(.status == $s)] | length;
      "| \(.[0].project) | \(count("expected")) | \(count("unexpected")) | \(count("flaky")) | \(count("skipped")) |"' <<<"$tests"
  jq -r '
    def listed($heading; $s): [.[] | select(.status == $s)]
      | if length == 0 then empty
        else "\n### \($heading)\n", (.[] | "- \(.project) › \(.path)\(if .error == "" then "" else ": " + .error end)")
        end;
    listed("Failed"; "unexpected"), listed("Passed only on retry"; "flaky")' <<<"$tests"
}

case "$RUN" in
  performance) performance ;;
  cross-browser) cross_browser ;;
esac
