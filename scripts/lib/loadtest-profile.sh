#!/usr/bin/env bash
# Read a load profile's own numbers, for the shell steps that enforce them.
#
# The profile is the single source of truth for what a run asks for and what it
# is judged against. Two shell steps need those numbers — the one that decides
# whether tonight breached a limit, and the one that compares tonight against
# the last fortnight — and both used to carry numbers of their own instead. One
# measurement then had two limits in two files, and an edit to either did not do
# what it said.
#
# So the numbers are read from the profile, here, once. This file is sourced;
# it defines functions and runs nothing.

# profile_reader_available reports whether this machine can read a profile.
#
# A reader that cannot read must fail rather than answer. A gate that says yes
# when it could not ask is the false green this repository has a rule against,
# and it is the more dangerous half here: the caller is deciding whether a
# night's numbers were acceptable, so "I could not read the limits" must never
# arrive as "no limit was breached".
profile_reader_available() {
  python3 -c 'import yaml' >/dev/null 2>&1
}

# profile_gates prints a profile's limits as a JSON array of
# {series, metric, max, min, blocking}. Absent max or min come through as null,
# which is how "no ceiling" stays distinguishable from a ceiling of zero.
profile_gates() {
  local profile="$1"

  if [ ! -s "$profile" ]; then
    echo "::error::there is no profile at $profile, so the numbers a night is judged against are unknown." >&2
    return 2
  fi
  if ! profile_reader_available; then
    echo "::error::this machine cannot read a profile (python3 with PyYAML is missing), so the limits could not be asked for — which is not the same as no limit being breached." >&2
    return 2
  fi

  python3 - "$profile" <<'PY'
import json
import sys

import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    profile = yaml.safe_load(handle) or {}

rows = []
for gate in profile.get("gates") or []:
    rows.append(
        {
            "series": gate.get("series"),
            "metric": gate.get("metric"),
            "max": gate.get("max"),
            "min": gate.get("min"),
            "blocking": bool(gate.get("blocking")),
        }
    )
json.dump(rows, sys.stdout)
PY
}

# profile_ungated prints the measurements a profile has deliberately left
# without a limit, as a JSON array of {series, metric, reason}.
#
# The set of absolute limits the profile took over had a catch-all: a series
# nobody listed was held to a default automatically. A profile has no catch-all,
# so a measurement that is neither limited nor named here is one nobody has
# ruled on — which looks exactly like one deliberately left alone and is not the
# same thing.
profile_ungated() {
  local profile="$1"

  if [ ! -s "$profile" ]; then
    echo "::error::there is no profile at $profile." >&2
    return 2
  fi
  if ! profile_reader_available; then
    echo "::error::this machine cannot read a profile (python3 with PyYAML is missing)." >&2
    return 2
  fi

  python3 - "$profile" <<'PY'
import json
import sys

import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    profile = yaml.safe_load(handle) or {}

rows = []
for entry in profile.get("ungated") or []:
    rows.append(
        {
            "series": entry.get("series"),
            "metric": entry.get("metric"),
            "reason": entry.get("reason"),
        }
    )
json.dump(rows, sys.stdout)
PY
}
