"""The packages inside an image and the advisories on them.

Trivy reads an image's packages once per digest, which costs a download of every layer, and
that list is cached; each run then judges the cached list against the day's database in moments.
"""

import json
import os
import tempfile
import time

import cache
import fetch

PLATFORM = "linux/amd64"
ATTEMPTS = 3
# A read of the largest images outlasts trivy's own five-minute default.
READ_TIMEOUT = "30m"


def _listing(image):
    """IMAGE's packages; a download that fails is retried, since registries drop connections."""
    for attempt in range(1, ATTEMPTS + 1):
        with tempfile.TemporaryDirectory() as work:
            out = os.path.join(work, "packages.json")
            try:
                fetch.run(["trivy", "image", "--quiet", "--image-src", "docker,remote", "--platform", PLATFORM,
                           "--timeout", READ_TIMEOUT, "--cache-backend", "memory", "--list-all-pkgs",
                           "--format", "cyclonedx", "--output", out, str(image)], what=f"the packages of {image}")
            except fetch.Unreadable:
                if attempt == ATTEMPTS:
                    raise
                time.sleep(attempt * 10)
                continue
            with open(out, encoding="utf-8") as handle:
                return json.load(handle)
    raise fetch.Unreadable(f"the packages of {image} were not read")


def packages(image):
    """IMAGE's package list, IMAGE carrying its digest; read once per digest and platform."""
    if not image.digest:
        raise fetch.Unreadable(f"{image}: a package list is read only for a digest")
    return cache.remember("sbom", f"{image.registry}/{image.repository}@{image.digest}#{PLATFORM}",
                          lambda: _listing(image))


def fixable(image):
    """(package count, {(advisory, package): (installed, fixed)}) for each advisory on IMAGE that
    the image's own package source has a fixed release of."""
    listing = packages(image)
    with tempfile.TemporaryDirectory() as work:
        path = os.path.join(work, "packages.cdx.json")
        with open(path, "w", encoding="utf-8") as handle:
            json.dump(listing, handle)
        report = fetch.parse_json(
            fetch.run(["trivy", "sbom", "--quiet", "--scanners", "vuln", "--format", "json", path],
                      what=f"the advisories on {image}"), f"the advisories on {image}")
    found = {}
    for result in report.get("Results") or []:
        for vulnerability in result.get("Vulnerabilities") or []:
            if vulnerability.get("FixedVersion"):
                key = (vulnerability["VulnerabilityID"], vulnerability["PkgName"])
                found[key] = (vulnerability.get("InstalledVersion", ""), vulnerability["FixedVersion"])
    return len(listing.get("components") or []), found
