#!/usr/bin/env bash
# The largest fleet each venue has been observed holding with arrivals landing and errors at zero.
# Sourced by scripts/tests/loadtest-venue-ceiling.test.sh, which holds each profile to its row.

loadtest_venues() {
  echo "staging runner"
}

loadtest_venue_ceiling_agents() {
  case "${1:?venue required}" in
    # The cluster pod: both generator pods share the 150 millicores the node has left.
    staging) echo 500 ;;
    # The throwaway stack on a GitHub-hosted runner, which brings its own processors and disk.
    runner) echo 8000 ;;
    *)
      echo "no ceiling has been recorded for venue: $1" >&2
      return 1
      ;;
  esac
}

# loadtest_venue_ceiling_evidence VENUE prints the run that established the ceiling.
loadtest_venue_ceiling_evidence() {
  case "${1:?venue required}" in
    staging)
      echo "load-test run 34831825889, 2026-09-14: the normal profile filed 500 of 500 machines, every phase at an error rate of zero, with the generator pod reporting 92.5% of its own processor still free."
      ;;
    runner)
      echo "perf-stack run 34850289658, 2026-09-14: the breakpoint ladder held step-8000 at an error rate of zero and gave at step-16000 with 0.330, past the 0.050 that profile calls giving out; the volume leg beside it filed 7,997 of 8,000 with registration's tail at 396ms."
      ;;
    *)
      echo "no evidence has been recorded for venue: $1" >&2
      return 1
      ;;
  esac
}
