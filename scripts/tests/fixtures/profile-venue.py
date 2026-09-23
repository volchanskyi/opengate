"""Print the text of every workflow job that names a given profile path.

A job is the unit that matters: the generator install and the fold of what it
timed are steps of the same job as the run itself, and a workflow-wide search
would find a sibling job's generator and call the question answered.

A matrix entry naming the path stands for the job that consumes it, so a job
whose matrix names the profile is printed whole.

The exception is a path named in the workflow's own `env`, which every job in
that workflow inherits. There the profile is workflow-scoped, so the honest
answer is workflow-scoped too and the whole file is printed.
"""

import pathlib
import sys

import yaml

workflows = pathlib.Path(sys.argv[1])
wanted = sys.argv[2]

for path in sorted(workflows.glob("*.yml")):
    text = path.read_text(encoding="utf-8")
    document = yaml.safe_load(text) or {}
    if wanted in yaml.safe_dump(document.get("env") or {}):
        print(f"# {path.name}: named in the workflow's own env")
        print(text)
        continue
    for name, job in (document.get("jobs") or {}).items():
        if not isinstance(job, dict):
            continue
        rendered = yaml.safe_dump(job)
        if wanted in rendered:
            print(f"# {path.name}:{name}")
            print(rendered)
