"""The release an advisory's ranges name as the fix for a version in use."""

from versions import Version


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
