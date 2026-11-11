#!/usr/bin/env python3
"""Stands in for gh in the dependency-fixes tests: `gh api [--paginate] PATH` answers with the file
FAKE_GH_DIR names for PATH, `?` written as `%3F`, and an unknown path fails as gh does on a 404.
Paginated, an object with a `files` list prints once per hundred files, as gh does for a commit.
Every call is appended to FAKE_GH_LOG when it is set."""

import json
import os
import sys

PAGE = 100


def pages(answer):
    files = answer.get("files") if isinstance(answer, dict) else None
    if not isinstance(files, list) or len(files) <= PAGE:
        return [answer]
    return [dict(answer, files=files[at : at + PAGE]) for at in range(0, len(files), PAGE)]


def main(argv):
    paginate = argv[1:2] == ["--paginate"]
    if argv[:1] != ["api"] or len(argv) != (3 if paginate else 2):
        sys.stderr.write(f"fake gh: unexpected call {argv}\n")
        return 64
    route = argv[-1]
    if os.environ.get("FAKE_GH_LOG"):
        with open(os.environ["FAKE_GH_LOG"], "a", encoding="utf-8") as log:
            log.write(f"{route}\n")
    path = os.path.join(os.environ["FAKE_GH_DIR"], route.replace("?", "%3F"))
    if not os.path.isfile(path):
        sys.stderr.write(f"gh: Not Found (HTTP 404) for {route}\n")
        return 1
    with open(path, encoding="utf-8") as handle:
        text = handle.read()
    if not paginate:
        sys.stdout.write(text)
        return 0
    for page in pages(json.loads(text)):
        sys.stdout.write(json.dumps(page) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
