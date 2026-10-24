"""Go modules as the module proxy publishes them: the releases of a module, and the module file
of one release, which names every module that release builds with."""

import functools

import cache
import fetch
import versions

PROXY = "https://proxy.golang.org"


def _escape(module):
    return "".join(f"!{c.lower()}" if c.isupper() else c for c in module)


@functools.lru_cache(maxsize=None)
def releases(module):
    """Every tagged release of MODULE the proxy lists, oldest first."""
    listing = fetch.http_text(f"{PROXY}/{_escape(module)}/@v/list")
    found = [versions.parse(line.strip()) for line in listing.splitlines()]
    return [str(v) for v in sorted(v for v in found if v and v.final)]


def requirements(module, version):
    """Every (module, version) the module file of MODULE at VERSION requires, replacements
    applied; the file never changes once published, so it is cached."""
    tag = version if version.startswith("v") else f"v{version}"
    text = cache.remember("gomod", f"{module}@{tag}",
                          lambda: fetch.http_text(f"{PROXY}/{_escape(module)}/@v/{tag}.mod"))
    return parse_requirements(text)


def parse_requirements(text):
    """Every (module, version) a module file requires, its replacements applied."""
    required, replaced, block = {}, {}, None
    for raw in text.splitlines():
        line = raw.split("//", 1)[0].rstrip()
        stripped = line.strip()
        if stripped.endswith("("):
            block = stripped.split()[0]
            continue
        if stripped == ")":
            block = None
            continue
        words = stripped.split()
        if not words:
            continue
        verb = block if block and words[0] not in ("require", "replace") else words[0]
        rest = words if block and words[0] not in ("require", "replace") else words[1:]
        if verb == "require" and len(rest) >= 2:
            required[rest[0]] = rest[1]
        elif verb == "replace" and "=>" in rest:
            arrow = rest.index("=>")
            target = rest[arrow + 1:]
            if len(target) == 2:
                replaced[rest[0]] = (target[0], target[1])
    return sorted(replaced.get(name, (name, version)) for name, version in required.items())
