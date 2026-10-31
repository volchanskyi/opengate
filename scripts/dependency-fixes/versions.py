"""Version arithmetic shared by every reader: ordering, and the line a release belongs to.

A line is the major and minor number together, so a patch release is one of the same line.
"""

import functools
import re

import versiontail

_LEADING = re.compile(r"^[vV]?(\d+(?:\.\d+)*)(.*)$")


@functools.total_ordering
class Version:
    """A release number with an optional pre-release, post-release or build tail."""

    def __init__(self, text):
        self.text = str(text).strip()
        match = _LEADING.match(self.text)
        if not match:
            raise ValueError(f"{self.text!r} is not a version")
        self.release = tuple(int(part) for part in match.group(1).split("."))
        self.rank, self.ids = versiontail.tail(match.group(2))

    def _cmp(self, other):
        width = max(len(self.release), len(other.release))
        left = self.release + (0,) * (width - len(self.release))
        right = other.release + (0,) * (width - len(other.release))
        if left != right:
            return -1 if left < right else 1
        if self.rank != other.rank:
            return -1 if self.rank < other.rank else 1
        return versiontail.compare_identifiers(self.ids, other.ids)

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
        return self.rank >= versiontail.FINAL

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
