#!/usr/bin/env bash
# Lists each failure in a go test -json stream with its output, as GitHub log groups, so a failure
# reads in the job log. A package that fails with no test failing names the tests still running.
# Usage: scripts/go-test-failures.sh <results.json>

set -euo pipefail

RESULTS="${1:-}"
if [ -z "$RESULTS" ] || [ ! -f "$RESULTS" ]; then
  echo "usage: scripts/go-test-failures.sh <results.json>" >&2
  exit 2
fi

# A timed-out test prints the panic and records no verdict, so unfinished tests carry the output.
# A build failure's compiler output is keyed by the build's import path, which the failure names.
jq -rs '
  def lines: .Output | rtrimstr("\n");
  def member($list; $x): any($list[]; . == $x);
  def entry($name; out): "::error title=Go test failed::\($name)", "::group::\($name)", out, "::endgroup::";
  . as $events
  | ($events | map(select(.Action == "fail" and .Test != null))) as $tests
  | ($tests | map(.Package) | unique) as $named
  | ($tests[] | . as $f
      | entry("\($f.Package) \($f.Test)";
          $events[] | select(.Action == "output" and .Package == $f.Package and .Test == $f.Test) | lines)),
    ($events[] | select(.Action == "fail" and .Test == null and (member($named; .Package) | not))
      | . as $f
      | ($events | map(select(.Package == $f.Package and .Test != null
          and (.Action == "pass" or .Action == "fail" or .Action == "skip")) | .Test) | unique) as $done
      | ($events | map(select(.Package == $f.Package and .Action == "run") | .Test) | unique
          | map(select(member($done; .) | not))) as $running
      | entry("\($f.Package), the package itself"
          + (if $running == [] then "" else " (still running: \($running | join(", ")))" end);
          ($events[] | select(.Action == "build-output" and $f.FailedBuild != null
            and .ImportPath == $f.FailedBuild) | lines),
          ($events[] | select(.Action == "output" and .Package == $f.Package
            and (.Test == null or member($running; .Test))) | lines)))
' "$RESULTS"
