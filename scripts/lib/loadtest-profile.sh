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

# profile_phases prints a profile's walk as a JSON array of
# {name, seconds, arrivals_per_second, sessions, agents, measured}.
#
# It is what a browser-side generator is handed. The technician numbers — how
# many journeys a second arrive, how many sessions are open — are technician-side
# facts the machine-side harness cannot offer, and they were read by nothing at
# all: a profile could declare fifteen arrivals a second while the run offered a
# fixed twenty virtual users sleeping a second and a half between journeys, and
# no number anywhere said the two disagreed.
# A second argument says how many seconds of the walk have already gone. The
# machine-side harness starts walking as soon as it has machines, and a
# browser-side generator cannot start until the estate it reads is filed — which
# is after the arrivals. A generator that then started the walk from its
# beginning would be a phase behind for the rest of the night: its steady window
# would run on past the drain, and the percentile it publishes would be taken
# partly against a fleet that had already left.
profile_phases() {
  local profile="$1" elapsed="${2:-0}"

  if [ ! -s "$profile" ]; then
    echo "::error::there is no profile at $profile, so the load a night offers is unknown." >&2
    return 2
  fi
  if ! profile_reader_available; then
    echo "::error::this machine cannot read a profile (python3 with PyYAML is missing), so the load to offer could not be asked for." >&2
    return 2
  fi

  python3 - "$profile" "$elapsed" <<'PROFILE_PHASES_PY'
import json
import re
import sys

import yaml

UNITS = {"ms": 0.001, "s": 1, "m": 60, "h": 3600}


def seconds(duration):
    """Read a phase duration the way the harness reads it: a sum of amounts with
    units, so 1m30s is ninety seconds and a bare number is refused."""
    if duration is None:
        raise SystemExit("a phase with no duration offers load for no time")
    text = str(duration).strip()
    total = 0.0
    matched = 0
    for amount, unit in re.findall(r"([0-9]+(?:\.[0-9]+)?)(ms|h|m|s)", text):
        total += float(amount) * UNITS[unit]
        matched += len(amount) + len(unit)
    if matched != len(text) or total <= 0:
        raise SystemExit(f"phase duration {text!r} is not a duration")
    return total


with open(sys.argv[1], encoding="utf-8") as handle:
    profile = yaml.safe_load(handle) or {}

elapsed = float(sys.argv[2]) if len(sys.argv) > 2 else 0.0

rows = []
for phase in profile.get("phases") or []:
    length = seconds(phase.get("duration"))
    if elapsed > 0:
        # Drop what is already over and shorten the phase that is running, so
        # the generator joins the walk where the walk actually is.
        if elapsed >= length:
            elapsed -= length
            continue
        length -= elapsed
        elapsed = 0.0
    rows.append(
        {
            "name": phase.get("name"),
            "seconds": length,
            "arrivals_per_second": float(phase.get("operator_arrivals_per_second") or 0),
            "sessions": int(phase.get("sessions") or 0),
            "agents": int(phase.get("connected_agents") or 0),
            "measured": bool(phase.get("measured")),
        }
    )
if not rows:
    raise SystemExit(
        "the walk was already over before the generator could join it: nothing "
        "is left of the profile to offer"
    )
json.dump(rows, sys.stdout)
PROFILE_PHASES_PY
}
