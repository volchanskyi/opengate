# Fix: merge-to-main + release-agent pipeline

## Context

Two problems:
1. **merge-to-main fails**: `GITHUB_TOKEN` can't push to `main` (branch protection requires 14 status checks on the merge commit). This blocks the entire release pipeline.
2. **release-agent never triggers**: Even when auto-tag creates a `v*` tag, the push event from a workflow doesn't trigger `release-agent.yml`.

**Constraint**: `main` must keep required status checks because PRs can be created directly to it. We can't weaken branch protection.

**Root cause of double-trigger attempts**: Any PAT push to `main` triggers all workflows listening on `push: branches: [main]` — CI re-runs, build-image runs twice, CD runs twice.

## Solution

Use `SYNC_TOKEN` (PAT) for merge-to-main push **and** remove `main` from push triggers in CI and build-image. PRs to main are still protected by the `pull_request` trigger.

## Changes

### 1. `ci.yml` — merge-to-main: use SYNC_TOKEN (line 644)

```yaml
token: ${{ secrets.GITHUB_TOKEN }}
→
token: ${{ secrets.SYNC_TOKEN }}
```

### 2. `ci.yml` — Remove `main` from push trigger (line 5)

```yaml
branches: [dev, main, dependabot-dev]
→
branches: [dev, dependabot-dev]
```

PR trigger keeps `main` — PRs to main still get full CI:
```yaml
pull_request:
  branches: [main, dev, dependabot-dev]  # unchanged
```

### 3. `build-image.yml` — Remove `push: branches: [main]` (lines 4-5)

```yaml
on:
  push:
    branches: [main]        # DELETE these 2 lines
  workflow_run:              # this becomes the only auto-trigger
    workflows: [CI]
    types: [completed]
    branches: [dev, dependabot-dev]
  workflow_dispatch:         # manual trigger stays
```

build-image already triggers via `workflow_run` on dev CI completion. The `push: main` trigger was redundant and is now the source of double runs.

### 4. `ci.yml` — auto-tag: add dispatch step (already committed in 6fb31ad)

Already done. The auto-tag job has:
- `id: push` on "Commit changelog and tag" step
- `pushed=true/false` output
- "Trigger release workflow" step: `gh workflow run release-agent.yml -f tag=$TAG`

### 5. `release-agent.yml` — workflow_dispatch trigger (already committed in 6fb31ad)

Already done:
- `workflow_dispatch` with `tag` input
- Checkout: `ref: ${{ inputs.tag || github.ref }}`
- Release: `tag_name: ${{ inputs.tag || github.ref_name }}`

### 6. Manual release for existing v0.1.0

After changes reach main:
```bash
gh workflow run release-agent.yml -f tag=v0.1.0
```

## Files to modify

- `.github/workflows/ci.yml` — line 5 (push trigger), line 644 (merge token)
- `.github/workflows/build-image.yml` — lines 4-5 (remove push trigger)

## Event flow after fix

```
Push to dev
  → CI runs on dev (push trigger)
  → merge-to-main pushes to main (SYNC_TOKEN)
    → No CI on main (removed from push triggers)
    → No build-image from push (removed)
  → auto-tag creates version tag + dispatches release-agent
  → CI completes on dev → build-image via workflow_run (single run)
  → build-image completes → CD triggers (single run)

PR to main
  → CI runs (pull_request trigger, unchanged)
  → Branch protection satisfied
```

## Verification

1. `actionlint` on ci.yml and build-image.yml
2. Push to dev, confirm CI runs only once
3. merge-to-main succeeds (SYNC_TOKEN bypasses protection)
4. build-image triggers once (via workflow_run only, not push)
5. auto-tag creates tag + dispatches release-agent
6. release-agent builds binaries + creates GitHub Release
7. Open a test PR to main — confirm CI runs via pull_request trigger
