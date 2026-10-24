#!/usr/bin/env python3
"""Stands in for curl in the dependency-fixes tests, answering as the real services do.

A GET answers with the file FAKE_HTTP_DIR names for the address, its scheme dropped and `?`
written as `%3F`; an address with no file answers 404, as curl --fail reports one. A HEAD, and a
GET asked to dump its headers, answer with that file's `.headers` beside it. OSV's batch
query answers each query from FAKE_HTTP_DIR/osv-matches.json, keyed `ecosystem|name|version` or
`commit|sha`, which lists the advisories OSV names for that release. Every call is appended to
FAKE_HTTP_LOG.
"""

import json
import os
import sys


def main(argv):
    headers, body, head, out, dump, url = [], None, False, None, None, None
    words = iter(argv)
    for word in words:
        if word in ("--header", "-H"):
            headers.append(next(words))
        elif word == "--data-binary":
            body = sys.stdin.read() if next(words) == "@-" else ""
        elif word == "--head":
            head = True
        elif word == "-o":
            out = next(words)
        elif word in ("--dump-header", "-D"):
            dump = next(words)
        elif word in ("--retry", "--retry-delay", "--connect-timeout", "--max-time"):
            next(words)
        elif not word.startswith("-"):
            url = word
    with open(os.environ["FAKE_HTTP_LOG"], "a", encoding="utf-8") as log:
        log.write(f"{'HEAD' if head else 'POST' if body is not None else 'GET'} {url}\n")
    root = os.environ["FAKE_HTTP_DIR"]
    if url == "https://api.osv.dev/v1/querybatch":
        with open(os.path.join(root, "osv-matches.json"), encoding="utf-8") as handle:
            matches = json.load(handle)
        results = []
        for query in json.loads(body)["queries"]:
            if "commit" in query:
                key = f"commit|{query['commit']}"
            else:
                key = f"{query['package']['ecosystem']}|{query['package']['name']}|{query.get('version', '')}"
            found = matches.get(key, [])
            results.append({"vulns": [{"id": i, "modified": "2026-10-23T00:00:00Z"} for i in found]})
        sys.stdout.write(json.dumps({"results": results}))
        return 0
    path = os.path.join(root, url.split("://", 1)[1].replace("?", "%3F"))
    if head:
        path += ".headers"
    if not os.path.isfile(path):
        sys.stderr.write(f"curl: (22) The requested URL returned error: 404 for {url}\n")
        return 22
    if dump:
        answer = path + ".headers"
        with open(dump, "w", encoding="utf-8") as handle:
            handle.write(open(answer, encoding="utf-8").read() if os.path.isfile(answer) else "HTTP/2 200\r\n\r\n")
    with open(path, "rb") as handle:
        content = handle.read()
    if out:
        with open(out, "wb") as handle:
            handle.write(content)
    else:
        sys.stdout.buffer.write(content)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
