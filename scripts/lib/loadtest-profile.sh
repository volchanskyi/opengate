#!/usr/bin/env bash
# Reads a load profile's own numbers, the one source of what a run asks for and is judged against.
# Sourced by the shell steps that enforce them; it defines functions and runs nothing.

# profile_reader_available reports whether this machine can read a profile; callers fail when it
# cannot, since an unread limit must never pass as an unbreached one.
profile_reader_available() {
  python3 -c 'import yaml' >/dev/null 2>&1
}

# profile_gates prints a profile's limits as a JSON array of {series, metric, max, min, blocking};
# an absent max or min is null, which keeps "no ceiling" distinct from a ceiling of zero.
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

# profile_ungated prints the measurements a profile leaves without a limit, as a JSON array of
# {series, metric, reason}; a measurement neither limited nor listed here is unruled.
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

# profile_venue prints the venue a profile runs in, which decides how large a fleet it may ask for.
profile_venue() {
  local profile="$1" venue

  if [ ! -s "$profile" ]; then
    echo "::error::there is no profile at $profile, so the place it runs is unknown." >&2
    return 2
  fi
  if ! profile_reader_available; then
    echo "::error::this machine cannot read a profile (python3 with PyYAML is missing), so the venue could not be asked for." >&2
    return 2
  fi

  venue="$(
    python3 - "$profile" <<'PY'
import sys

import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    profile = yaml.safe_load(handle) or {}

print(profile.get("environment") or "")
PY
  )"
  if [ -z "$venue" ]; then
    echo "::error::$profile names no environment, so nothing can say where it runs." >&2
    return 2
  fi
  printf '%s\n' "$venue"
}

# profile_is_ladder prints true when a profile declares what counts as giving out, else false.
profile_is_ladder() {
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
import sys

import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    profile = yaml.safe_load(handle) or {}

print("true" if profile.get("gave_out") else "false")
PY
}

# profile_phases prints a profile's walk as a JSON array of {name, seconds, arrivals_per_second,
# sessions, agents, measured}; the optional second argument is the seconds of the walk already gone.
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
