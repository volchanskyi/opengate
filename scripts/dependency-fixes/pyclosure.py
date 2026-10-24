"""Prints, as JSON, every distribution a Python tool's environment installed for it.

Run by the tool's own interpreter, so markers are judged as that environment judges them.
Each entry names its version and what each requirer in the closure asks of it.

Usage: <the tool's python> -E pyclosure.py DISTRIBUTION
"""

import importlib.metadata as metadata
import json
import re
import sys

try:
    from packaging.requirements import Requirement
except ImportError:
    from pip._vendor.packaging.requirements import Requirement


def normal(name):
    return re.sub(r"[-_.]+", "-", name).lower()


def closure(root):
    found, asks, queue = {}, {}, [(root, None)]
    while queue:
        name, extra = queue.pop()
        key = normal(name)
        try:
            dist = metadata.distribution(name)
        except metadata.PackageNotFoundError:
            if key == normal(root):
                raise
            continue
        first = key not in found
        found[key] = dist.version
        if not first and extra is None:
            continue
        for text in dist.requires or []:
            requirement = Requirement(text)
            environment = {"extra": extra or ""}
            if requirement.marker and not requirement.marker.evaluate(environment):
                continue
            asks.setdefault(normal(requirement.name), []).append([key, str(requirement.specifier)])
            queue.append((requirement.name, None))
            for wanted in requirement.extras:
                queue.append((requirement.name, wanted))
    return [{"name": name, "version": version, "asks": asks.get(name, [])}
            for name, version in sorted(found.items())]


if __name__ == "__main__":
    print(json.dumps(closure(sys.argv[1])))
