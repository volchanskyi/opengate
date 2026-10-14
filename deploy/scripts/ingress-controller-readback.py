"""Compares what the cluster's ingress controller holds with what the repository declares.

Prints one line per difference and exits 1 when there is any.

Usage: ingress-controller-readback.py RENDERED_CONFIGMAP LIVE_CONFIGMAP VALUES SUBNET_KEY SUBNET
                                      LIVE_SERVICE RELEASES PINNED_CHART
"""

import json
import sys

import yaml

RELEASE = "ingress-nginx"


def read_yaml(path):
    with open(path, encoding="utf-8") as handle:
        return yaml.safe_load(handle) or {}


def read_json(path):
    with open(path, encoding="utf-8") as handle:
        text = handle.read()
    return json.loads(text) if text.strip() else {}


def configmap_differences(declared, held):
    """The ConfigMap's data equals the chart's render exactly; snippets are named on their own."""
    found = []
    if held.get("allow-snippet-annotations") == "true":
        found.append("the controller ConfigMap allows snippet annotations")
    for key in sorted(set(declared) | set(held)):
        if key not in held:
            found.append(f"the controller ConfigMap lacks {key}, declared {declared[key]!r}")
        elif key not in declared:
            found.append(f"the controller ConfigMap holds {key}={held[key]!r}, which the chart does not render")
        elif held[key] != declared[key]:
            found.append(f"the controller ConfigMap's {key} is {held[key]!r}, declared {declared[key]!r}")
    return found


def service_differences(wanted, held):
    """Every declared annotation is on the Service with its declared value; others are the chart's."""
    return [
        f"the controller Service's {key} is {held.get(key)!r}, declared {value!r}"
        for key, value in sorted(wanted.items())
        if held.get(key) != value
    ]


def main(argv):
    rendered, live_configmap, values, subnet_key, subnet, live_service, releases, pinned = argv
    declared = read_yaml(rendered).get("data") or {}
    held = read_json(live_configmap).get("data") or {}
    found = configmap_differences(declared, held)

    controller = read_yaml(values).get("controller") or {}
    wanted = {key: str(value) for key, value in ((controller.get("service") or {}).get("annotations") or {}).items()}
    wanted[subnet_key] = subnet
    annotations = (read_json(live_service).get("metadata") or {}).get("annotations") or {}
    found += service_differences(wanted, annotations)

    running = [entry.get("chart") for entry in read_json(releases) or [] if entry.get("name") == RELEASE]
    if running != [pinned]:
        found.append(f"the {RELEASE} release runs {', '.join(running) or 'no chart'}, pinned {pinned}")

    for line in found:
        print(line)
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
