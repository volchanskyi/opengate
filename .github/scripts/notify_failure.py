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
LABEL = os.environ.get("FAILURE_LABEL", "ci-failure")
ANSI_RE = re.compile(r"\x1b\[[0-9;]*[a-zA-Z]")

log = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# gh CLI wrapper
# ---------------------------------------------------------------------------

def gh(*args: str) -> tuple[str, str, int]:
    """Run a ``gh`` CLI command and return (stdout, stderr, returncode).

    stderr is returned rather than dropped: when a call is refused, the reason
    is the only thing that says why, and an issue filed without it describes a
    failure nobody can diagnose once the run's logs have expired.
    """
    result = subprocess.run(
        ["gh", *args],
        capture_output=True,
        text=True,
    )
    return result.stdout.strip(), result.stderr.strip(), result.returncode


# ---------------------------------------------------------------------------
# Data collection
# ---------------------------------------------------------------------------

def fetch_failed_jobs(repo: str, run_id: str) -> list[dict[str, Any]]:
    """Return a list of job dicts whose conclusion is ``failure``."""
    jq_filter = (
        ".jobs[] | {id, name, conclusion, html_url, "
        'steps: [.steps[] | select(.conclusion == "failure") | .name]}'
    )
    stdout, _, _ = gh(
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


def fetch_job_log(repo: str, job_id: int) -> tuple[list[str], list[str]]:
    """Fetch log output for one completed job, returning (lines, refusals).

    Two routes reach the same log, and a job that failed always logged
    something, so an empty answer is a refusal rather than a job that said
    nothing. The archive endpoint (``jobs/{id}/logs``) is tried first — it
    serves a completed job while the run around it is still in progress, which
    is the only condition this ever runs under. ``gh run view --log`` is the
    second route, reaching the log by a different path when the first is
    refused.

    The archive endpoint is asked twice. Almost every job here sets a colour
    terminal — cargo and go both do — so almost every log carries escape
    sequences, and ``gh`` refuses to hand such a body over unless asked to. The
    second route drops the flag, for a ``gh`` old enough not to know it. Only
    then does the third route try the other door, which cannot help while the
    run is in progress but is what reaches a log once it is not.

    ``refusals`` carries what each route said when it produced nothing, so a
    log that cannot be read is reported with its reason instead of as silence.
    """
    refusals: list[str] = []
    logs = f"repos/{repo}/actions/jobs/{job_id}/logs"
    routes = (
        (
            "jobs/{id}/logs --allow-escape-sequences",
            ("api", logs, "--allow-escape-sequences"),
        ),
        ("jobs/{id}/logs", ("api", logs)),
        (
            "run view --log",
            ("run", "view", "--repo", repo, "--log", "--job", str(job_id)),
        ),
    )
    for name, args in routes:
        stdout, stderr, rc = gh(*args)
        if rc == 0 and stdout and not is_storage_error(stdout):
            return ANSI_RE.sub("", stdout).splitlines(), []
        reason = stderr or first_line(stdout) or f"exit {rc}, no output"
        refusals.append(f"{name}: {reason}")
    return [], refusals


def is_storage_error(body: str) -> bool:
    """Whether body is blob storage's error document rather than a log.

    The archive endpoint redirects to storage, and when the blob behind it is
    gone the redirect target answers with an XML error document — on stdout,
    exit zero. Filing that as a job's log records a failure nobody can read.
    """
    head = body.lstrip()[:512]
    return "<Error>" in head and "<Code>" in head


def first_line(body: str) -> str:
    """The first non-empty line of body, for reporting what a route answered."""
    for line in body.splitlines():
        if line.strip():
            return line.strip()[:200]
    return ""


# ---------------------------------------------------------------------------
# Markdown body
# ---------------------------------------------------------------------------

def build_issue_body(
    *,
    job_name: str,
    job_url: str,
    failed_steps: list[str],
    log_lines: list[str],
    log_refusals: list[str],
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
    if excerpt_lines:
        excerpt = "\n".join(excerpt_lines)
    else:
        # The issue outlives the run's logs, so a log that could not be read
        # says which routes were tried and what each one answered. "No log
        # output available." on its own describes nothing and is what every
        # issue said for months.
        excerpt = "\n".join(
            ["Could not read this job's log. Each route was tried and refused:"]
            + [f"  - {r}" for r in log_refusals]
        )

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
    workflow: str,
) -> None:
    """Create a new issue or comment on an existing open one."""
    title = f"{workflow} failure on {branch} in {job_name}"
    short_sha = sha[:7]

    existing, _, _ = gh(
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

    unreadable: list[str] = []
    for job in failed_jobs:
        job_name: str = job["name"]
        job_id: int = job["id"]
        log_lines, log_refusals = fetch_job_log(args.repo, job_id)
        if not log_lines:
            unreadable.append(job_name)
            for refusal in log_refusals:
                log.error("no log for job '%s' via %s", job_name, refusal)
        body = build_issue_body(
            job_name=job_name,
            job_url=job["html_url"],
            failed_steps=job.get("steps", []),
            log_lines=log_lines,
            log_refusals=log_refusals,
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
            workflow=args.workflow,
        )

    # The issue is filed either way — a failure with no log is still worth
    # recording — but a step that could not do the one thing it exists to do
    # does not report success. A green step is how this went unnoticed until two
    # staging failures had aged past their log retention with nothing to read.
    if unreadable:
        log.error(
            "::error::no log could be read for: %s", ", ".join(unreadable)
        )
        sys.exit(1)


if __name__ == "__main__":
    main()
