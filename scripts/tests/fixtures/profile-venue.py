"""Print the text of every workflow job that names a given profile path.

A job is the unit that matters: the generator install and the fold of what it
timed are steps of the same job as the run itself, and a workflow-wide search
would find a sibling job's generator and call the question answered.

A matrix entry naming the path stands for the job that consumes it, so a job
whose matrix names the profile is printed whole.

The exception is a path named in the workflow's own `env`, which every job in
that workflow inherits. There the profile is workflow-scoped, so the honest
answer is workflow-scoped too and the whole file is printed.

A job that calls a workflow of this repository runs that workflow's jobs as its
own, so the called workflow is printed after it, and so is every workflow it
calls in turn.
"""

import pathlib
import sys

import yaml

workflows = pathlib.Path(sys.argv[1])
wanted = sys.argv[2]


def print_called(caller, called, seen):
    """Print the workflow a job calls, then every workflow that one calls, each once."""
    if not called.startswith("./.github/workflows/"):
        return
    target = workflows / pathlib.Path(called).name
    print(f"# {caller} calls {target.name}")
    if target.name in seen or not target.is_file():
        print("")
        return
    seen.add(target.name)
    text = target.read_text(encoding="utf-8")
    print(text)
    for name, job in ((yaml.safe_load(text) or {}).get("jobs") or {}).items():
        if isinstance(job, dict):
            print_called(f"{target.name}:{name}", str(job.get("uses") or ""), seen)


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
            print_called(f"{path.name}:{name}", str(job.get("uses") or ""), set())
