#!/usr/bin/env bash
# Permits or refuses this runner for a speed job, by the processor model lscpu names: a model
# holding an entry of scripts/lib/refused-processors.sh is refused.
#
# Usage:
#   scripts/runner-processor.sh >>"$GITHUB_OUTPUT"
#
# Exit codes:
#   0  read; prints permitted=true|false and processor=<model>
#   2  lscpu names no processor model
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/refused-processors.sh
. "$HERE/lib/refused-processors.sh"

model="$(lscpu | sed -n 's/^Model name:[[:space:]]*//p' | head -n 1)"
if [ -z "$model" ]; then
  echo "::error::lscpu names no processor model on this runner" >&2
  exit 2
fi

permitted=true
while IFS= read -r refused; do
  case "$model" in *"$refused"*) permitted=false ;; esac
done < <(refused_processors)

echo "permitted=$permitted"
echo "processor=$model"
if [ "$permitted" = false ]; then
  echo "::notice::this runner's $model is refused; the next try asks for a fresh runner" >&2
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    printf -- '- Refused a runner with an %s; the next try asks for a fresh runner.\n' "$model" \
      >>"$GITHUB_STEP_SUMMARY"
  fi
fi
