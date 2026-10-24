#!/usr/bin/env python3
"""Stands in for gh in the dependency-fixes tests: `gh api --paginate PATH` answers with the file
FAKE_GH_DIR names for PATH, `?` written as `%3F`, and an unknown path fails as gh does on a 404.
Every call is appended to FAKE_GH_LOG when it is set."""

import os
import sys


def main(argv):
    if argv[:2] != ["api", "--paginate"] or len(argv) != 3:
        sys.stderr.write(f"fake gh: unexpected call {argv}\n")
        return 64
    if os.environ.get("FAKE_GH_LOG"):
        with open(os.environ["FAKE_GH_LOG"], "a", encoding="utf-8") as log:
            log.write(f"{argv[2]}\n")
    path = os.path.join(os.environ["FAKE_GH_DIR"], argv[2].replace("?", "%3F"))
    if not os.path.isfile(path):
        sys.stderr.write(f"gh: Not Found (HTTP 404) for {argv[2]}\n")
        return 1
    with open(path, encoding="utf-8") as handle:
        sys.stdout.write(handle.read())
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
