"""Checks every dependency of the repository for a published advisory that a patch release of
the line in use fixes, and refuses while one exists.

Usage:
  python3 scripts/dependency-fixes [READER...]       (every reader, and the sweep, when none is named)
  python3 scripts/dependency-fixes images-declared   (each image the repository declares, one a line)
  python3 scripts/dependency-fixes images-undeclared (each image on stdin the repository does not)
  python3 scripts/dependency-fixes images-unbuilt PLATFORM... (each image the cluster deploy
                                                     installs with no build for one of PLATFORMS)
  python3 scripts/dependency-fixes declarations      (each declaration no reader reads)
  python3 scripts/dependency-fixes image-findings REPORT (a Trivy JSON report judged against the
                                                     exception list)

Exit codes:
  0  no advisory a patch release fixes, no serious one outside a live exception, and every
     declaration is read
  1  an advisory a patch release fixes, a serious one outside a live exception, an exception that
     lapsed, a declaration no reader reads, an undeclared image, or an image the cluster deploy
     installs with no build for a node's platform
  2  a lookup gave no answer the check can read, or the exception list or a report is malformed
"""

import os
import sys

import datetime

import actions
import charts
import chartsource
import declarations
import exceptions
import fetch
import golang
import goproxy
import imagerefs
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
# The charts the deploy installs and the scripts that start pods in the cluster.
DEPLOYED = ("deploy/helm/", "deploy/scripts/")


def declared_images(root):
    """Every image the repository declares, the charts' renders included, without digests."""
    named = set(imagerefs.references(root))
    for _, version, url, name in chartsource.charts(root):
        named |= set(chartsource.rendered(root, url, name, version))
    return sorted({str(registry.parse(reference).at(registry.parse(reference).tag)) for reference in named})


def undeclared_images(root, lines):
    """Every image in LINES, a cluster's running images, that the repository neither declares nor
    builds; a digest pins a tag the repository declares, so only the tag is compared."""
    running = sorted({line.strip() for line in lines if line.strip()})
    if not running:
        raise fetch.Unreadable("no running image was handed to the comparison")
    declared = set(declared_images(root))
    owned = [entry["image"] for entry in imagerefs.scope().get("images", [])]
    return [reference for reference in running
            if str(registry.parse(reference).at(registry.parse(reference).tag)) not in declared
            and not any(reference.startswith(prefix) for prefix in owned)]


def deployed_images(root):
    """Every image the cluster deploy installs: those its charts and pod scripts name, and those the
    cluster's own chart releases render."""
    named = {reference for reference, paths in imagerefs.references(root).items()
             if any(path.startswith(DEPLOYED) for path in paths)}
    for _, version, url, name in chartsource.charts(root):
        named |= set(chartsource.rendered(root, url, name, version))
    if not named:
        raise fetch.Unreadable("no image the cluster deploy installs was found")
    return sorted(named)


def unbuilt_images(root, wanted):
    """(image, platforms of WANTED it lacks, platforms it has) for each image the cluster deploy
    installs with no build for one of WANTED, and how many images were read."""
    deployed = deployed_images(root)
    found = []
    for reference in deployed:
        image = registry.pinned(registry.parse(reference))
        built = registry.platforms(image)
        if not wanted <= built:
            found.append((image.at(image.tag), sorted(wanted - built), sorted(built)))
    return found, len(deployed)


def unread_lines(root):
    unread = declarations.unread(root)
    if not unread:
        return [], 0
    return ([f"dependency-fixes: {len(unread)} declarations no reader reads; give each a reader, or list "
             "a file that only looks like one in scripts/dependency-fixes/scope.json:"]
            + [f"  {line}" for line in unread]), 1


def today():
    return datetime.datetime.now(datetime.timezone.utc).date().isoformat()


def newest_release(package):
    """A Go module's newest release, None for a package no module proxy publishes."""
    if not exceptions.go_modules({"package": package}) or package == "stdlib":
        return None
    found = goproxy.releases(package)
    return found[-1] if found else None


def lapsed_lines(root, readings):
    """Marks the serious Go findings a live exception names, and lines for each that lapsed."""
    findings = [finding for reading in readings for finding in reading.findings]
    problems = exceptions.apply(exceptions.load(root), findings, today(), newest_release, exceptions.go_modules)
    if not problems:
        return [], 0
    return [f"dependency-fixes: {len(problems)} exceptions lapsed:"] + [f"  {p}" for p in problems], 1


def image_findings(root, path):
    """The image scan's report judged against the exception list."""
    with open(path, encoding="utf-8") as handle:
        scan = fetch.parse_json(handle.read(), path)
    return exceptions.judge_image(scan, exceptions.load(root), today(), newest_release)


def check(root, chosen):
    readings = []
    for name in chosen:
        reading = READERS[name](root)
        if reading.read == 0:
            raise fetch.Unreadable(f"the {name} reader read no dependencies at all")
        readings.append(reading)
    lapsed, lapse_status = lapsed_lines(root, readings) if "go" in chosen else ([], 0)
    lines, status = report.render(readings)
    lines, status = lines + lapsed, max(status, lapse_status)
    if chosen == list(READERS):
        unread, refused = unread_lines(root)
        lines, status = lines + unread, max(status, refused)
    return lines, status


def _images_declared(_args):
    print("\n".join(declared_images(ROOT)))
    return 0


def _declarations(_args):
    lines, status = unread_lines(ROOT)
    print("\n".join(lines or ["dependency-fixes: every declaration is one a reader reads"]))
    return status


def _images_unbuilt(args):
    wanted = set(args)
    found, read = unbuilt_images(ROOT, wanted)
    for image, missing, built in found:
        print(f"::error::dependency-fixes: {image} has no build for {', '.join(missing)}; "
              f"it is built for {', '.join(built)}")
    if not found:
        print(f"dependency-fixes: every one of the {read} images the deploy installs has a build for "
              f"{', '.join(sorted(wanted))}")
    return 1 if found else 0


def _image_findings(args):
    lines, status = image_findings(ROOT, args[0])
    print("\n".join(lines))
    return status


def _images_undeclared(_args):
    found = undeclared_images(ROOT, sys.stdin.read().splitlines())
    for reference in found:
        print(f"::error::dependency-fixes: the cluster runs {reference}, which nothing in the "
              "repository declares")
    return 1 if found else 0


def _readers(chosen):
    """Runs the readers CHOSEN names, every reader and the sweep when it names none."""
    chosen = chosen or list(READERS)
    unknown = [name for name in chosen if name not in READERS]
    if unknown:
        print(f"dependency-fixes: no reader named {', '.join(unknown)}; readers: {', '.join(READERS)}",
              file=sys.stderr)
        return 2
    lines, status = check(ROOT, chosen)
    print("\n".join(lines))
    return status


# Each command beside the arguments it takes; a command given others is read as a reader's name.
COMMANDS = {
    "images-declared": (_images_declared, lambda args: not args),
    "declarations": (_declarations, lambda args: not args),
    "images-unbuilt": (_images_unbuilt, lambda args: len(args) > 0),
    "image-findings": (_image_findings, lambda args: len(args) == 1),
    "images-undeclared": (_images_undeclared, lambda args: not args),
}


def main(argv):
    command, takes = COMMANDS.get(argv[0], (None, None)) if argv else (None, None)
    try:
        if command and takes(argv[1:]):
            return command(argv[1:])
        return _readers(argv)
    except (fetch.Unreadable, exceptions.Malformed) as error:
        print(f"::error::dependency-fixes: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
