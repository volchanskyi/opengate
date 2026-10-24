"""Tools released only as tagged source on GitHub: a tag's commit, and the tags of a line."""

import re

import fetch
import versions


def template_pattern(template):
    """A regular expression matching the tags TEMPLATE names, its {v} captured."""
    head, _, tail = template.partition("{v}")
    return re.compile("^" + re.escape(head) + r"(\d+(?:\.\d+)*)" + re.escape(tail) + "$")


def releases(repository, template):
    """Every (version, tag) of REPOSITORY whose tag fits TEMPLATE, oldest first."""
    pattern = template_pattern(template)
    found = []
    for tag in fetch.github(f"repos/{repository}/tags?per_page=100"):
        match = pattern.match(tag.get("name", ""))
        if match:
            found.append((versions.Version(match.group(1)), tag["name"], tag["commit"]["sha"]))
    return [(str(v), name, sha) for v, name, sha in sorted(found, key=lambda item: item[0])]
