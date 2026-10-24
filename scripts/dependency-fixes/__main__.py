"""Checks every dependency of the repository for a published advisory that a patch release of
the line in use fixes, and refuses while one exists.

Usage:
  python3 scripts/dependency-fixes [READER...]       (every reader, and the sweep, when none is named)
  python3 scripts/dependency-fixes images-declared   (each image the repository declares, one a line)
  python3 scripts/dependency-fixes images-undeclared (each image on stdin the repository does not)
  python3 scripts/dependency-fixes declarations      (each declaration no reader reads)

Exit codes:
  0  no advisory a patch release fixes, and every declaration is read
  1  an advisory a patch release fixes, a declaration no reader reads, or an undeclared image
  2  a lookup gave no answer the check can read
"""

import os
import sys

import actions
import charts
import declarations
import fetch
import golang
import images
import npm
import registry
import report
import rust
import terraform
import tools

READERS = {
    "npm": npm.read,
    "go": golang.read,
    "rust": rust.read,
    "tools": tools.read,
    "actions": actions.read,
    "terraform": terraform.read,
    "charts": charts.read,
    "images": images.read,
}
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def declared_images(root):
    """Every image the repository declares, the charts' renders included, without digests."""
    named = set(images.references(root))
    for _, version, url, name in charts.charts(root):
        named |= set(charts.rendered(root, url, name, version))
    return sorted({str(registry.parse(reference).at(registry.parse(reference).tag)) for reference in named})


def undeclared_images(root, lines):
    """Every image in LINES, a cluster's running images, that the repository neither declares nor
    builds; a digest pins a tag the repository declares, so only the tag is compared."""
    running = sorted({line.strip() for line in lines if line.strip()})
    if not running:
        raise fetch.Unreadable("no running image was handed to the comparison")
    declared = set(declared_images(root))
    owned = [entry["image"] for entry in images.scope().get("images", [])]
    return [reference for reference in running
            if str(registry.parse(reference).at(registry.parse(reference).tag)) not in declared
            and not any(reference.startswith(prefix) for prefix in owned)]


def unread_lines(root):
    unread = declarations.unread(root)
    if not unread:
        return [], 0
    return ([f"dependency-fixes: {len(unread)} declarations no reader reads; give each a reader, or list "
             "a file that only looks like one in scripts/dependency-fixes/scope.json:"]
            + [f"  {line}" for line in unread]), 1


def check(root, chosen):
    readings = []
    for name in chosen:
        reading = READERS[name](root)
        if reading.read == 0:
            raise fetch.Unreadable(f"the {name} reader read no dependencies at all")
        readings.append(reading)
    lines, status = report.render(readings)
    if chosen == list(READERS):
        unread, refused = unread_lines(root)
        lines, status = lines + unread, max(status, refused)
    return lines, status


def main(argv):
    try:
        if argv == ["images-declared"]:
            print("\n".join(declared_images(ROOT)))
            return 0
        if argv == ["declarations"]:
            lines, status = unread_lines(ROOT)
            print("\n".join(lines or ["dependency-fixes: every declaration is one a reader reads"]))
            return status
        if argv == ["images-undeclared"]:
            found = undeclared_images(ROOT, sys.stdin.read().splitlines())
            for reference in found:
                print(f"::error::dependency-fixes: the cluster runs {reference}, which nothing in the "
                      "repository declares")
            return 1 if found else 0
        chosen = argv or list(READERS)
        unknown = [name for name in chosen if name not in READERS]
        if unknown:
            print(f"dependency-fixes: no reader named {', '.join(unknown)}; readers: {', '.join(READERS)}",
                  file=sys.stderr)
            return 2
        lines, status = check(ROOT, chosen)
    except fetch.Unreadable as error:
        print(f"::error::dependency-fixes: {error}", file=sys.stderr)
        return 2
    print("\n".join(lines))
    return status


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
