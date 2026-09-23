"""Say whether a load profile asks for technician load in any phase.

Prints `yes` when any phase declares a held session count or a rate of screens
opened, and `no` otherwise. Both are browser-side numbers a machine-side harness
cannot offer, so a profile that declares one needs a venue that runs a generator.
"""

import sys

import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    profile = yaml.safe_load(handle)

wants = any(
    (phase.get("sessions") or 0) > 0 or (phase.get("operator_arrivals_per_second") or 0) > 0
    for phase in profile.get("phases", []) or []
)
print("yes" if wants else "no")
