#!/usr/bin/env bash
# Refuses a run against staging whose server is another build than the one the run tests: each
# server pod's image names a commit that builds the same server as the commit under test.
#
# Usage:
#   UNDER_TEST=<commit|sha-commit> scripts/staging-version-check.sh >>"$GITHUB_OUTPUT"
#
# Environment:
#   NAMESPACE   namespace the staging release runs in (required)
#   RELEASE     the staging release's name (required)
#   UNDER_TEST  the commit the run tests, or its sha-<commit> image tag (required)
#
# Exit codes:
#   0  staging runs the build under test; prints running=<tag>
#   2  a required input is missing
#   3  staging runs another build, or which one cannot be told; prints not_started=<reason>
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/image-inputs.sh
. "$HERE/lib/image-inputs.sh"

NAMESPACE="${NAMESPACE:-}"
RELEASE="${RELEASE:-}"
UNDER_TEST="${UNDER_TEST:-}"
RUNNING_IMAGES=()

usage() {
  sed -n '5,6p' "${BASH_SOURCE[0]}" | sed 's/^# *//' >&2
  exit 2
}

# summary_line writes one line on the run's summary page, when the run has one.
summary_line() {
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    printf -- '- %s\n' "$1" >>"$GITHUB_STEP_SUMMARY"
  fi
}

# refuse hands the reason to the job's outputs, its errors and its summary page, then exits 3.
refuse() {
  echo "not_started=$1"
  echo "::error::the run did not start: $1" >&2
  summary_line "The run did not start: $1"
  exit 3
}

# commit_of resolves a commit, or a sha-<commit> image tag, to a commit this checkout holds.
commit_of() {
  git rev-parse --verify --quiet "${1#sha-}^{commit}"
}

# read_running_images sets RUNNING_IMAGES to each distinct image staging's server pods started from.
read_running_images() {
  local images err_file status=0 answer
  err_file="$(mktemp)"
  images="$(kubectl -n "$NAMESPACE" get pods \
    -l "app.kubernetes.io/instance=${RELEASE},app.kubernetes.io/component=server" \
    -o jsonpath='{.items[*].spec.containers[?(@.name=="server")].image}' 2>"$err_file")" \
    || status=$?
  # The credential plugin warns on every call, so only the cluster's own answer is kept.
  answer="$(grep -v '^Warning:' "$err_file" | paste -sd ' ' - || true)"
  rm -f "$err_file"
  if [ "$status" -ne 0 ]; then
    refuse "the cluster did not say which build staging runs (${answer:-kubectl exited $status})"
  fi
  mapfile -t RUNNING_IMAGES < <(tr ' ' '\n' <<<"$images" | sed '/^$/d' | sort -u)
}

main() {
  local var
  for var in NAMESPACE RELEASE UNDER_TEST; do
    if [ -z "${!var}" ]; then
      echo "staging-version-check: $var is required" >&2
      usage
    fi
  done

  read_running_images
  case "${#RUNNING_IMAGES[@]}" in
    0) refuse "no server pod runs in $NAMESPACE" ;;
    1) ;;
    *)
      refuse "staging's server pods run more than one build: $(printf '%s\n' "${RUNNING_IMAGES[@]##*:}" | paste -sd ' ' -)"
      ;;
  esac

  local tag="${RUNNING_IMAGES[0]##*:}" running under
  if ! [[ "$tag" =~ ^sha-[0-9a-f]{7,40}$ ]]; then
    refuse "staging runs ${RUNNING_IMAGES[0]}, whose tag names no commit"
  fi
  running="$(commit_of "$tag")" || refuse "staging runs $tag, a commit this checkout does not hold"
  under="$(commit_of "$UNDER_TEST")" \
    || refuse "the commit under test, $UNDER_TEST, is not in this checkout"

  local inputs=() changed
  mapfile -t inputs < <(image_inputs)
  changed="$(git diff --name-only "$running" "$under" -- "${inputs[@]}")"
  if [ -n "$changed" ]; then
    refuse "staging runs $tag, but commit ${under:0:7} builds a different server ($(grep -c . <<<"$changed") files differ); deploy ${under:0:7} to staging first"
  fi

  echo "running=$tag"
  summary_line "Staging runs $tag, the server that commit ${under:0:7} builds."
}

main "$@"
