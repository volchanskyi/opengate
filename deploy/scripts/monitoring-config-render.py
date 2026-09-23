"""Render a ConfigMap from a directory, or compare one against what came back.

With a name and a directory, prints the ConfigMap JSON that `kubectl apply`
should be given: every file in the directory as one key. A live copy on standard
input contributes its own labels and annotations, which is what keeps an apply
from stripping the ownership metadata Helm writes on the objects it owns — a
ConfigMap applied without them is one the next chart upgrade refuses to touch.

With `--same <desired>` and a live ConfigMap on standard input, exits zero when
the live copy carries exactly the desired data and non-zero otherwise. Only the
data is compared: the cluster adds a resource version of its own, and an apply
that changed nothing else is not a difference anybody asked about.
"""

import json
import pathlib
import sys


def data_of(directory: pathlib.Path) -> dict[str, str]:
    return {
        entry.name: entry.read_text(encoding="utf-8")
        for entry in sorted(directory.iterdir())
        if entry.is_file()
    }


def live_document() -> dict:
    if sys.stdin.isatty():
        return {}
    raw = sys.stdin.read().strip()
    return json.loads(raw) if raw else {}


if sys.argv[1] == "--same":
    desired = json.loads(sys.argv[2])
    live = live_document()
    sys.exit(0 if live.get("data") == desired.get("data") else 1)

name, directory = sys.argv[1], pathlib.Path(sys.argv[2])
metadata: dict = {"name": name}
live_metadata = live_document().get("metadata") or {}
for carried in ("labels", "annotations"):
    kept = {
        key: value
        for key, value in (live_metadata.get(carried) or {}).items()
        # The applied configuration the cluster records for us. Carrying our own
        # last apply forward into the next one nests it inside itself.
        if key != "kubectl.kubernetes.io/last-applied-configuration"
    }
    if kept:
        metadata[carried] = kept

print(
    json.dumps(
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": metadata,
            "data": data_of(directory),
        }
    )
)
