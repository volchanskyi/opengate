#!/usr/bin/env python3
"""Stands in for trivy in the dependency-fixes tests, answering as the real scanner does.

`trivy image` writes a CycloneDX package list naming the image it read, and fails as trivy does
when the read, FAKE_TRIVY_READ_MINUTES long, outlasts its --timeout (five minutes unless given).
`trivy sbom` reads one back and lists, with a fixed release, each advisory FAKE_TRIVY_FINDINGS (a
JSON file) names for that image's digest, beside one advisory its package source has no fix for.
Each call's verb and last argument are appended to FAKE_TRIVY_LOG.
"""

import json
import os
import re
import sys

UNITS = {"h": 60.0, "m": 1.0, "s": 1 / 60}


def minutes(duration):
    """The minutes a Go duration such as 30m0s names."""
    return sum(float(n) * UNITS[unit] for n, unit in re.findall(r"(\d+(?:\.\d+)?)([hms])", duration))


def main(args):
    with open(os.environ["FAKE_TRIVY_LOG"], "a", encoding="utf-8") as log:
        log.write(" ".join(args[:1] + args[-1:]) + "\n")
    if args[0] == "image":
        allowed = minutes(args[args.index("--timeout") + 1]) if "--timeout" in args else 5.0
        if float(os.environ.get("FAKE_TRIVY_READ_MINUTES", "0")) > allowed:
            sys.stderr.write(f"FATAL Fatal error: run error: image scan error: scan error: {args[-1]}: "
                             "context deadline exceeded\n")
            return 1
        with open(args[args.index("--output") + 1], "w", encoding="utf-8") as out:
            json.dump({"bomFormat": "CycloneDX", "metadata": {"component": {"name": args[-1]}},
                       "components": [{"name": "busybox"}, {"name": "openssl"}]}, out)
        return 0
    with open(args[-1], encoding="utf-8") as listing:
        digest = json.load(listing)["metadata"]["component"]["name"].rsplit("@", 1)[-1]
    with open(os.environ["FAKE_TRIVY_FINDINGS"], encoding="utf-8") as findings:
        found = json.load(findings).get(digest, [])
    vulns = [{"VulnerabilityID": v, "PkgName": "openssl", "InstalledVersion": "3.0.0-r0",
              "FixedVersion": "3.0.1-r0"} for v in found]
    vulns.append({"VulnerabilityID": "CVE-2026-0009", "PkgName": "busybox", "InstalledVersion": "1.37.0-r1"})
    print(json.dumps({"Results": [{"Target": "image", "Vulnerabilities": vulns}]}))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
