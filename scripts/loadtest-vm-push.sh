#!/usr/bin/env bash
# Convert canonical load-test rows to Prometheus text and push them to VM.
#
# The workload label rides with every sample so the gate can tell one scenario's
# work from the work it replaced. See workload_name in loadtest-summarize.sh.
set -euo pipefail

SUMMARY_FILE="${1:-loadtest-summary.json}"

# The consecutive-night counts the regression check wrote, which are what make an
# advisory that repeats into a finding rather than a line in a summary. Without
# them the check has no memory between nights and each bad night raises the
# comparison point the next one is judged against, until the advisory goes quiet
# with the slowdown recorded as normal.
STREAK_FILE="${2:-loadtest-p99-streaks.json}"

if [[ ! -f "$SUMMARY_FILE" ]]; then
  echo "missing: $SUMMARY_FILE" >&2
  exit 2
fi

metrics="$(
  jq -r '
    def label_escape:
      tostring
      | gsub("\\\\"; "\\\\")
      | gsub("\""; "\\\"");

    def sample($metric; $value):
      select($value != null)
      | "\($metric){commit=\"\(.commit | label_escape)\",env=\"\((.env // "ci") | label_escape)\",source=\"\((.source // "unknown") | label_escape)\",scenario=\"\((.scenario // "unknown") | label_escape)\",phase=\"\((.phase // "aggregate") | label_escape)\",workload=\"\((.workload // "unknown") | label_escape)\"} \($value)";

    .[]
    | sample("loadtest_latency_p50_ms"; .latency_p50_ms),
      sample("loadtest_latency_p95_ms"; .latency_p95_ms),
      sample("loadtest_latency_p99_ms"; .latency_p99_ms),
      sample("loadtest_rps"; .rps),
      sample("loadtest_error_rate"; .error_rate),
      sample("loadtest_dropped_iterations"; .dropped_iterations)
  ' "$SUMMARY_FILE"
)"

if [[ -z "$metrics" ]]; then
  echo "no load-test metrics generated from $SUMMARY_FILE" >&2
  exit 2
fi

# The counts ride on the same push, keyed the same way the window is, so the next
# night reads them with one query and nothing new has to persist.
if [[ -f "$STREAK_FILE" ]]; then
  streaks="$(
    jq -r --arg commit "${GITHUB_SHA:-unknown}" '
      def label_escape:
        tostring
        | gsub("\\\\"; "\\\\")
        | gsub("\""; "\\\"");

      .[]
      | "loadtest_p99_advisory_streak{commit=\"\($commit | label_escape)\",env=\"ci\",source=\"\(.source | label_escape)\",scenario=\"\(.scenario | label_escape)\",phase=\"\(.phase | label_escape)\",workload=\"\((.workload // "") | label_escape)\"} \(.streak)"
    ' "$STREAK_FILE"
  )"
  if [[ -n "$streaks" ]]; then
    metrics="$metrics"$'\n'"$streaks"
  fi
fi

# shellcheck source=lib/vm-push.sh
source "$(dirname "$0")/lib/vm-push.sh"
printf '%s\n' "$metrics" | vm_push
