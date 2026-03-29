# Plan: Merge Go + Rust Benchmark Workflows

## Context
Currently two separate workflow files (`bench-go.yml` and `bench-rust.yml`) have identical triggers, permissions, and concurrency settings. Merging them into one file reduces duplication and makes the benchmark pipeline easier to maintain.

## Changes

### 1. Create `.github/workflows/bench.yml`
Single workflow with two jobs:

```yaml
name: Benchmarks

on:
  workflow_run:
    workflows: [CI]
    types: [completed]
    branches: [dev]

permissions:
  contents: write

concurrency:
  group: gh-pages-deploy
  cancel-in-progress: false

jobs:
  bench-go:
    # ... existing Go bench job (unchanged steps)

  bench-rust:
    needs: bench-go          # <-- sequential: Rust runs after Go completes
    # ... existing Rust bench job (unchanged steps)
```

Jobs run sequentially — Rust benchmarks start only after Go benchmarks finish. This avoids concurrent gh-pages pushes.

### 2. Delete old files
- `.github/workflows/bench-go.yml`
- `.github/workflows/bench-rust.yml`

## Files Modified
- **Create**: `.github/workflows/bench.yml`
- **Delete**: `.github/workflows/bench-go.yml`, `.github/workflows/bench-rust.yml`

## Verification
- `actionlint .github/workflows/bench.yml` passes
- `make lint` passes (includes actionlint)
- Confirm both jobs appear in the workflow with correct steps
