"""The part of a version after its release number: a development, pre-release or post-release
tail, ranked against the bare release, and compared identifier by identifier."""

import re

_PEP440_PRE = re.compile(r"^[.-]?(a|alpha|b|beta|c|rc|pre|preview)[.-]?(\d*)", re.IGNORECASE)
_PEP440_POST = re.compile(r"^[.-]?(post|rev|r)[.-]?(\d*)", re.IGNORECASE)
_PEP440_DEV = re.compile(r"^[.-]?dev[.-]?(\d*)", re.IGNORECASE)

# A tail sorts below the bare release when it is a development or pre-release, above when post.
DEV, PRE, FINAL, POST = 0, 1, 2, 3


def _identifiers(text):
    return tuple(int(part) if part.isdigit() else part for part in re.split(r"[.\-]", text) if part)


def tail(text):
    """(rank, identifiers) of the text after a release number; a build suffix is ignored."""
    text = text.split("+", 1)[0]
    if not text:
        return (FINAL, ())
    for pattern, rank in ((_PEP440_DEV, DEV), (_PEP440_POST, POST)):
        match = pattern.match(text)
        if match and match.end() == len(text):
            return (rank, _identifiers(match.group(match.lastindex) or "0"))
    match = _PEP440_PRE.match(text)
    if match and match.end() == len(text):
        return (PRE, (match.group(1).lower(), int(match.group(2) or 0)))
    if text[0] in "-.":
        return (PRE, _identifiers(text[1:]))
    return (PRE, _identifiers(text))


def _compare_one(a, b):
    if isinstance(a, int) and isinstance(b, int):
        return -1 if a < b else 1
    if isinstance(a, int):
        return -1
    if isinstance(b, int):
        return 1
    return -1 if a < b else 1


def compare_identifiers(left, right):
    """-1, 0 or 1 as LEFT sorts below, with or above RIGHT; a number sorts below a word."""
    for a, b in zip(left, right):
        if a != b:
            return _compare_one(a, b)
    return (len(left) > len(right)) - (len(left) < len(right))
