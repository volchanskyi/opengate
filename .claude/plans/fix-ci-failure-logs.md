# Plan: Fix missing logs in CI failure issues

## Context
The `notify_failure.py` script creates GitHub issues for CI failures, but the error log section always shows "No log output available." (see issue #54).

**Root cause**: `fetch_failed_logs()` on line 71-83 calls `gh run view <run_id> --log-failed`, which downloads the **full run's log archive**. This fails silently (returns empty stdout) because the workflow run is still in progress when `notify-failure` executes — GitHub only makes the log archive available after the run completes. The `gh()` wrapper on line 33-40 discards the error.

## Fix
Replace `gh run view --log-failed` with per-job log fetching via `gh api repos/{repo}/actions/jobs/{job_id}/logs`. Individual job logs ARE available once a job completes, even while the overall run is still active.

### File: `.github/scripts/notify_failure.py`

**Change 1**: Update `fetch_failed_jobs()` (line 47-68) to also return `id` in the jq filter, so we have the numeric job ID.

```python
jq_filter = (
    ".jobs[] | {id, name, conclusion, html_url, "
    'steps: [.steps[] | select(.conclusion == "failure") | .name]}'
)
```

**Change 2**: Replace `fetch_failed_logs()` (line 71-83) with a function that fetches logs per-job using the job ID:

```python
def fetch_job_log(repo: str, job_id: int) -> list[str]:
    """Fetch log output for a single completed job."""
    stdout, rc = gh(
        "api", f"repos/{repo}/actions/jobs/{job_id}/logs",
    )
    if rc != 0 or not stdout:
        return []
    return stdout.splitlines()
```

**Change 3**: Update `main()` loop (line 228-251) to call the per-job fetch instead of the bulk fetch:

```python
for job in failed_jobs:
    job_name: str = job["name"]
    job_id: int = job["id"]
    log_lines = fetch_job_log(args.repo, job_id)
    body = build_issue_body(
        ...
        log_lines=log_lines,
        ...
    )
```

Remove the bulk `job_logs = fetch_failed_logs(...)` call on line 226.

## Verification
1. Run the script locally against a known failed run to confirm logs are populated:
   ```
   python3 .github/scripts/notify_failure.py --repo volchanskyi/opengate --run-id 22793896541 --branch main --workflow CI --sha d9a97e0 --event schedule --run-url https://github.com/volchanskyi/opengate/actions/runs/22793896541
   ```
   (dry-run / inspect output before it creates an issue)
2. `actionlint` passes
3. Push to `dev` and wait for a failure scenario (or manually trigger)
