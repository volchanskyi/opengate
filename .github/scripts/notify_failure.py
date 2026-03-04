#!/usr/bin/env python3
"""Create or update GitHub Issues for failed CI jobs.

Queries the GitHub Actions API for failed jobs in a workflow run,
fetches their log output, and creates one issue per failed job
(or comments on an existing open issue for the same job/branch).

Requires the ``gh`` CLI to be authenticated (GH_TOKEN env var).
"""
from __future__ import annotations

import argparse
import json
import logging
import os
import re
import subprocess
import sys
from typing import Any

MAX_LOG_LINES = 80
MAX_BODY_LENGTH = 60_000
LABEL = "ci-failure"
ANSI_RE = re.compile(r"\x1b\[[0-9;]*[a-zA-Z]")

log = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# gh CLI wrapper
# ---------------------------------------------------------------------------

def gh(*args: str) -> tuple[str, int]:
    """Run a ``gh`` CLI command and return (stdout, returncode)."""
    result = subprocess.run(
        ["gh", *args],
        capture_output=True,
        text=True,
    )
    return result.stdout.strip(), result.returncode


# ---------------------------------------------------------------------------
# Data collection
# ---------------------------------------------------------------------------

def fetch_failed_jobs(repo: str, run_id: str) -> list[dict[str, Any]]:
    """Return a list of job dicts whose conclusion is ``failure``."""
    jq_filter = (
        ".jobs[] | {name, conclusion, html_url, "
        'steps: [.steps[] | select(.conclusion == "failure") | .name]}'
    )
    stdout, _ = gh(
        "api", f"repos/{repo}/actions/runs/{run_id}/jobs",
        "--paginate", "--jq", jq_filter,
    )
    failed: list[dict[str, Any]] = []
    for line in stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            job = json.loads(line)
        except json.JSONDecodeError:
            continue
        if job.get("conclusion") == "failure":
            failed.append(job)
    return failed


def fetch_failed_logs(repo: str, run_id: str) -> dict[str, list[str]]:
    """Parse ``gh run view --log-failed`` into per-job log line lists."""
    stdout, _ = gh("run", "view", run_id, "--log-failed", "--repo", repo)
    stdout = ANSI_RE.sub("", stdout)

    job_logs: dict[str, list[str]] = {}
    for raw_line in stdout.splitlines():
        parts = raw_line.split("\t", 3)
        if len(parts) < 4:
            continue
        job_name, _step, _ts, log_line = parts
        job_logs.setdefault(job_name, []).append(log_line)
    return job_logs


# ---------------------------------------------------------------------------
# Markdown body
# ---------------------------------------------------------------------------

def build_issue_body(
    *,
    job_name: str,
    job_url: str,
    failed_steps: list[str],
    log_lines: list[str],
    workflow: str,
    branch: str,
    sha: str,
    event: str,
    run_url: str,
    server_url: str,
    repo: str,
) -> str:
    """Assemble the GitHub Issue body for a single failed job."""
    short_sha = sha[:7]
    commit_url = f"{server_url}/{repo}/commit/{sha}"
    steps_str = ", ".join(failed_steps) if failed_steps else "unknown"

    excerpt_lines = log_lines[-MAX_LOG_LINES:]
    excerpt = "\n".join(excerpt_lines) if excerpt_lines else "No log output available."

    body = (
        f"## CI Failure: {job_name}\n"
        f"\n"
        f"**Workflow**: {workflow}\n"
        f"**Branch**: `{branch}`\n"
        f"**Commit**: [`{short_sha}`]({commit_url})\n"
        f"**Trigger**: {event}\n"
        f"**Run**: [{run_url}]({run_url})\n"
        f"**Job**: [{job_name}]({job_url})\n"
        f"**Failed Step(s)**: {steps_str}\n"
        f"\n"
        f"### Error Log (last {MAX_LOG_LINES} lines)\n"
        f"\n"
        f"<details>\n"
        f"<summary>Expand log</summary>\n"
        f"\n"
        f"```\n"
        f"{excerpt}\n"
        f"```\n"
        f"\n"
        f"</details>\n"
    )

    if len(body) > MAX_BODY_LENGTH:
        body = body[:MAX_BODY_LENGTH] + f"\n\n_Log truncated. [Full run]({run_url})_"
    return body


# ---------------------------------------------------------------------------
# Issue creation / dedup
# ---------------------------------------------------------------------------

def create_or_comment_issue(
    repo: str,
    branch: str,
    job_name: str,
    body: str,
    run_id: str,
    run_url: str,
    sha: str,
) -> None:
    """Create a new issue or comment on an existing open one."""
    title = f"CI failure on {branch} in {job_name}"
    short_sha = sha[:7]

    existing, _ = gh(
        "issue", "list",
        "--repo", repo,
        "--label", LABEL,
        "--state", "open",
        "--search", f"{title} in:title",
        "--limit", "1",
        "--json", "number",
        "--jq", ".[0].number // empty",
    )

    if existing.strip():
        issue_num = existing.strip()
        comment = f"New failure in run [{run_id}]({run_url}) \u2014 {short_sha}\n\n{body}"
        gh("issue", "comment", issue_num, "--repo", repo, "--body", comment)
        log.info("Commented on issue #%s for job '%s'", issue_num, job_name)
    else:
        gh(
            "issue", "create",
            "--repo", repo,
            "--title", title,
            "--body", body,
            "--label", LABEL,
        )
        log.info("Created issue for job '%s'", job_name)


# ---------------------------------------------------------------------------
# CLI + main
# ---------------------------------------------------------------------------

def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    """Parse CLI arguments with env-var fallbacks."""
    p = argparse.ArgumentParser(
        description="Create GitHub Issues for failed CI jobs.",
    )
    p.add_argument("--repo", default=os.environ.get("REPO"), required=False)
    p.add_argument("--run-id", default=os.environ.get("RUN_ID"), required=False)
    p.add_argument("--branch", default=os.environ.get("BRANCH"), required=False)
    p.add_argument("--workflow", default=os.environ.get("WORKFLOW"), required=False)
    p.add_argument("--sha", default=os.environ.get("SHA"), required=False)
    p.add_argument("--event", default=os.environ.get("EVENT"), required=False)
    p.add_argument("--run-url", default=os.environ.get("RUN_URL"), required=False)
    p.add_argument(
        "--server-url",
        default=os.environ.get("GITHUB_SERVER_URL", "https://github.com"),
    )
    args = p.parse_args(argv)

    missing = [
        name
        for name in ("repo", "run_id", "branch", "workflow", "sha", "event", "run_url")
        if getattr(args, name) is None
    ]
    if missing:
        p.error(f"missing required arguments: {', '.join(missing)}")
    return args


def main(argv: list[str] | None = None) -> None:
    """Entry point."""
    logging.basicConfig(format="%(message)s", level=logging.INFO)
    args = parse_args(argv)

    failed_jobs = fetch_failed_jobs(args.repo, args.run_id)
    if not failed_jobs:
        log.info("No failed jobs found \u2014 nothing to do.")
        sys.exit(0)

    job_logs = fetch_failed_logs(args.repo, args.run_id)

    for job in failed_jobs:
        job_name: str = job["name"]
        body = build_issue_body(
            job_name=job_name,
            job_url=job["html_url"],
            failed_steps=job.get("steps", []),
            log_lines=job_logs.get(job_name, []),
            workflow=args.workflow,
            branch=args.branch,
            sha=args.sha,
            event=args.event,
            run_url=args.run_url,
            server_url=args.server_url,
            repo=args.repo,
        )
        create_or_comment_issue(
            repo=args.repo,
            branch=args.branch,
            job_name=job_name,
            body=body,
            run_id=args.run_id,
            run_url=args.run_url,
            sha=args.sha,
        )


if __name__ == "__main__":
    main()
