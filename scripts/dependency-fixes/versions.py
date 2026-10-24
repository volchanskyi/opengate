"""Version arithmetic shared by every reader: ordering, the line a release belongs to, and the
release an advisory's ranges name as the fix for a version in use.

A line is the major and minor number together, so a patch release is one of the same line.
"""

import functools
import re

_LEADING = re.compile(r"^[vV]?(\d+(?:\.\d+)*)(.*)$")
_PEP440_PRE = re.compile(r"^[.-]?(a|alpha|b|beta|c|rc|pre|preview)[.-]?(\d*)", re.IGNORECASE)
_PEP440_POST = re.compile(r"^[.-]?(post|rev|r)[.-]?(\d*)", re.IGNORECASE)
_PEP440_DEV = re.compile(r"^[.-]?dev[.-]?(\d*)", re.IGNORECASE)

# A tail sorts below the bare release when it is a development or pre-release, above when post.
_DEV, _PRE, _FINAL, _POST = 0, 1, 2, 3


def _identifiers(text):
    return tuple(int(part) if part.isdigit() else part for part in re.split(r"[.\-]", text) if part)


def _tail(text):
    text = text.split("+", 1)[0]
    if not text:
        return (_FINAL, ())
    for pattern, rank in ((_PEP440_DEV, _DEV), (_PEP440_POST, _POST)):
        match = pattern.match(text)
        if match and match.end() == len(text):
            return (rank, _identifiers(match.group(match.lastindex) or "0"))
    match = _PEP440_PRE.match(text)
    if match and match.end() == len(text):
        return (_PRE, (match.group(1).lower(), int(match.group(2) or 0)))
    if text[0] in "-.":
        return (_PRE, _identifiers(text[1:]))
    return (_PRE, _identifiers(text))


def _compare_identifiers(left, right):
    for a, b in zip(left, right):
        if a == b:
            continue
        if isinstance(a, int) and isinstance(b, int):
            return -1 if a < b else 1
        if isinstance(a, int):
            return -1
        if isinstance(b, int):
            return 1
        return -1 if a < b else 1
    return (len(left) > len(right)) - (len(left) < len(right))


@functools.total_ordering
class Version:
    """A release number with an optional pre-release, post-release or build tail."""

    def __init__(self, text):
        self.text = str(text).strip()
        match = _LEADING.match(self.text)
        if not match:
            raise ValueError(f"{self.text!r} is not a version")
        self.release = tuple(int(part) for part in match.group(1).split("."))
        self.rank, self.ids = _tail(match.group(2))

    def _cmp(self, other):
        width = max(len(self.release), len(other.release))
        left = self.release + (0,) * (width - len(self.release))
        right = other.release + (0,) * (width - len(other.release))
        if left != right:
            return -1 if left < right else 1
        if self.rank != other.rank:
            return -1 if self.rank < other.rank else 1
        return _compare_identifiers(self.ids, other.ids)

    def __eq__(self, other):
        return self._cmp(other) == 0

    def __lt__(self, other):
        return self._cmp(other) < 0

    def __hash__(self):
        return hash((self.release + (0,) * (6 - len(self.release)), self.rank, self.ids))

    def __repr__(self):
        return self.text

    @property
    def final(self):
        """A release rather than a development or pre-release build."""
        return self.rank >= _FINAL

    @property
    def line(self):
        """The major and minor number, which every patch release of this one shares."""
        return (self.release + (0, 0))[:2]


def parse(text):
    """The Version TEXT names, or None when it names none."""
    try:
        return Version(text)
    except ValueError:
        return None


def newest_patch(releases, version):
    """The newest of RELEASES on VERSION's line when it is later than VERSION, else VERSION."""
    current = Version(version)
    same = [Version(v) for v in releases if Version(v).line == current.line and Version(v) > current]
    return str(max(same)) if same else version


def _event_key(event):
    kind, value = next(iter(event.items()))
    floor = value == "0" and kind == "introduced"
    return (Version("0") if floor else Version(value), 0 if kind == "introduced" else 1)


def _covering_fix(version, events):
    """For one range: None when VERSION is outside it, else the fixing release or ''."""
    inside = False
    for event in sorted((event for event in events if next(iter(event)) != "limit"), key=_event_key):
        kind, value = next(iter(event.items()))
        point = Version("0") if value == "0" else Version(value)
        if kind == "introduced":
            inside = inside or point <= version
            continue
        passed = point <= version if kind == "fixed" else point < version
        if passed:
            inside = False
            continue
        if not inside:
            return None
        return value if kind == "fixed" else ""
    return "" if inside else None


def fix_for(version, ranges):
    """The release that fixes VERSION across RANGES: a version string, '' when no release does,
    or None when no range covers it. A range of a type other than SEMVER or ECOSYSTEM is skipped.
    """
    version = Version(version)
    fixes = []
    for rng in ranges:
        if rng.get("type") not in ("SEMVER", "ECOSYSTEM"):
            continue
        fix = _covering_fix(version, rng.get("events") or [])
        if fix is None:
            continue
        if fix == "":
            return ""
        fixes.append(fix)
    if not fixes:
        return None
    return str(max(fixes, key=Version))
