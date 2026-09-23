"""Print the scaling profile's arrival verdict and the run's own write-off line.

Two tab-separated figures: the blocking limit on the machine-side aggregate's
error rate, and `safety.max_error_rate`. The second is also what decides whether
a phase measured the target at all, so a verdict stricter than it can fail a leg
the run has already certified.
"""

import sys

import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    profile = yaml.safe_load(handle)

limit = ""
for gate in profile.get("gates", []) or []:
    if gate.get("series") == "quic/quic-agents/aggregate" and gate.get("metric") == "error_rate":
        limit = gate.get("max", "")
        break

print(f"{limit}\t{profile.get('safety', {}).get('max_error_rate', '')}")
