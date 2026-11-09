"""The severity a CVSS version 3 vector scores, by the specification's base-score formula."""

import math

WEIGHTS = {
    "AV": {"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2},
    "AC": {"L": 0.77, "H": 0.44},
    "UI": {"N": 0.85, "R": 0.62},
    "C": {"H": 0.56, "L": 0.22, "N": 0.0},
    "I": {"H": 0.56, "L": 0.22, "N": 0.0},
    "A": {"H": 0.56, "L": 0.22, "N": 0.0},
}
# Privileges required weigh more when the hole reaches past its own component.
PRIVILEGES = {"U": {"N": 0.85, "L": 0.62, "H": 0.27}, "C": {"N": 0.85, "L": 0.68, "H": 0.5}}
BANDS = ((9.0, "CRITICAL"), (7.0, "HIGH"), (4.0, "MEDIUM"), (0.1, "LOW"))


def _round_up(value):
    """The specification's round-up to one decimal, exact where floating point is not."""
    scaled = round(value * 100000)
    if scaled % 10000 == 0:
        return scaled / 100000
    return (math.floor(scaled / 10000) + 1) / 10


def score(vector):
    """VECTOR's base score, or None for a vector that is not a complete version 3 one."""
    parts = vector.split("/")
    if not parts or parts[0] not in ("CVSS:3.0", "CVSS:3.1"):
        return None
    metrics = dict(part.split(":", 1) for part in parts[1:] if ":" in part)
    try:
        scope = metrics["S"]
        weights = {name: table[metrics[name]] for name, table in WEIGHTS.items()}
        privileges = PRIVILEGES[scope][metrics["PR"]]
    except KeyError:
        return None
    base_impact = 1 - (1 - weights["C"]) * (1 - weights["I"]) * (1 - weights["A"])
    if scope == "U":
        impact = 6.42 * base_impact
    else:
        impact = 7.52 * (base_impact - 0.029) - 3.25 * (base_impact - 0.02) ** 15
    exploitability = 8.22 * weights["AV"] * weights["AC"] * privileges * weights["UI"]
    if impact <= 0:
        return 0.0
    total = impact + exploitability if scope == "U" else 1.08 * (impact + exploitability)
    return _round_up(min(total, 10))


def rating(vector):
    """VECTOR's severity in the specification's words, or '' for one this cannot score."""
    value = score(vector)
    if value is None:
        return ""
    return next((name for floor, name in BANDS if value >= floor), "NONE")
