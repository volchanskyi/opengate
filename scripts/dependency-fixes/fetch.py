"""Every outside read the check makes: HTTP through curl, GitHub through gh, and local commands.

A read that fails, or answers with something that does not parse, raises Unreadable, which the
check reports as a refusal to answer and never as a clean result.
"""

import json
import subprocess
import tempfile

CURL = [
    "curl", "--fail", "--silent", "--show-error", "--location",
    "--retry", "4", "--retry-all-errors", "--retry-delay", "3",
    "--connect-timeout", "20", "--max-time", "180",
]


class Unreadable(Exception):
    """A lookup that gave no answer the check can read."""


def run(command, cwd=None, stdin=None, ok=(0,), what=None):
    """COMMAND's standard output; an exit status outside OK raises Unreadable."""
    try:
        done = subprocess.run(
            command, cwd=cwd, input=stdin, capture_output=True, check=False,
            text=not isinstance(stdin, bytes),
        )
    except FileNotFoundError as error:
        raise Unreadable(f"{what or command[0]}: {command[0]} is not installed") from error
    if done.returncode not in ok:
        detail = (done.stderr or done.stdout or "").strip().splitlines()
        raise Unreadable(f"{what or ' '.join(command)}: exit {done.returncode}: "
                         f"{' / '.join(detail[-3:]) if detail else 'no output'}")
    return done.stdout


def parse_json(text, what):
    try:
        return json.loads(text)
    except (TypeError, ValueError) as error:
        raise Unreadable(f"{what}: the answer is not JSON") from error


def json_stream(text, what):
    """Every JSON value in TEXT, which holds several written one after another."""
    decoder = json.JSONDecoder()
    values, index = [], 0
    while True:
        while index < len(text) and text[index].isspace():
            index += 1
        if index >= len(text):
            return values
        try:
            value, index = decoder.raw_decode(text, index)
        except ValueError as error:
            raise Unreadable(f"{what}: the answer is not a JSON stream") from error
        values.append(value)


def http_text(url, headers=()):
    command = list(CURL)
    for header in headers:
        command += ["--header", header]
    return run(command + [url], what=f"GET {url}")


def _last_headers(text):
    """The headers of the last response in TEXT, lower-cased, which curl writes one per hop."""
    blocks = text.replace("\r", "").strip().split("\n\n")
    found = {}
    for line in blocks[-1].splitlines()[1:]:
        key, _, value = line.partition(":")
        found[key.strip().lower()] = value.strip()
    return found


def http_headers(url, headers=()):
    """The response headers of a HEAD request, from the last hop of any redirect."""
    command = list(CURL) + ["--head"]
    for header in headers:
        command += ["--header", header]
    return _last_headers(run(command + [url], what=f"HEAD {url}"))


def http_page(url, headers=()):
    """The body of a GET and the headers it came with, from the last hop of any redirect."""
    with tempfile.NamedTemporaryFile(mode="r", encoding="utf-8", suffix=".headers") as dump:
        command = list(CURL) + ["--dump-header", dump.name]
        for header in headers:
            command += ["--header", header]
        body = run(command + [url], what=f"GET {url}")
        return body, _last_headers(dump.read())


def http_bytes(url):
    try:
        done = subprocess.run(CURL + [url], capture_output=True, check=False)
    except FileNotFoundError as error:
        raise Unreadable("curl is not installed") from error
    if done.returncode != 0:
        raise Unreadable(f"GET {url}: exit {done.returncode}: {done.stderr.decode(errors='replace').strip()}")
    return done.stdout


def http_json(url, headers=()):
    return parse_json(http_text(url, headers), f"GET {url}")


def http_post_json(url, body):
    command = CURL + ["--header", "Content-Type: application/json", "--data-binary", "@-", url]
    return parse_json(run(command, stdin=json.dumps(body), what=f"POST {url}"), f"POST {url}")


def github(path):
    """Every page of a GitHub API read, joined into one list when the pages are lists."""
    pages = json_stream(run(["gh", "api", "--paginate", path], what=f"gh api {path}"), f"gh api {path}")
    if pages and all(isinstance(page, list) for page in pages):
        return [item for page in pages for item in page]
    if len(pages) != 1:
        raise Unreadable(f"gh api {path}: {len(pages)} answers where one was expected")
    return pages[0]
