#!/usr/bin/env bash
# loadtest-venue-ceilings.sh — the largest fleet each venue has been shown to hold.
#
# Sourced by scripts/tests/loadtest-venue-ceiling.test.sh. NOT executable on its
# own — this is a table of facts with the runs that established them.
#
# Why it exists: the number of machines a profile asks for is the largest number
# in it and was the one number nothing checked. A profile naming a fleet its
# venue has never held does not fail loudly. It half-arrives, and a percentile
# taken over the half that did still clears a limit written for a full fleet —
# so the leg is green and the finding is that there is no finding. Both figures
# below cost a run to learn and neither was written anywhere a text sweep could
# read.
#
# A ceiling is a reading, not a target. It says what a venue has been observed
# holding with its arrivals landing and its errors at zero — never what it ought
# to manage. Raising a row means walking the rung, not deciding the venue should
# cope.
#
# Adding a venue: add its row, add the evidence beside it, and the sweep will
# hold every profile naming that venue to it. A venue no profile names fails the
# sweep rather than sitting here going stale.

# loadtest_venues — the venues a ceiling has been recorded for.
loadtest_venues() {
  echo "staging runner"
}

# loadtest_venue_ceiling_agents VENUE — the largest fleet that venue has held.
loadtest_venue_ceiling_agents() {
  case "${1:?venue required}" in
    # The cluster pod. The node offers 1,830 millicores with 1,680 already
    # spoken for, so both generator pods share 150 between them and the fleet
    # this holds is bounded by the pod rather than by the server it drives.
    staging) echo 500 ;;
    # The throwaway stack on a GitHub-hosted runner, which brings its own
    # processors and disk and is destroyed with the job.
    runner) echo 8000 ;;
    *)
      echo "no ceiling has been recorded for venue: $1" >&2
      return 1
      ;;
  esac
}

# loadtest_venue_ceiling_evidence VENUE — the run that established the figure.
#
# A number with no run behind it is a guess that has been written down, and the
# sweep refuses a row without one. The evidence names what the run read, because
# "it was green" and "its machines arrived" are different claims and only the
# second one is a ceiling.
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
