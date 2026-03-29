# Fix: merge-to-main coverage extraction failure

## Context
The `merge-to-main` job re-runs `go test` to extract the coverage percentage, then hides
all output with `> /dev/null 2>&1`. This fails with exit code 1 (cause unknown — suppressed).
The re-run is unnecessary: the `go` job already computed coverage on the same commit before
`merge-to-main` starts. The correct fix is to pass the value forward using GitHub Actions
[job outputs](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idoutputs),
eliminating the redundant test run and its failure point entirely.

## Changes — `.github/workflows/ci.yml` only

### 1. Go job — export coverage as job output

**Add `outputs:` block** directly under `go:` (before `name:`):
```yaml
go:
  name: Go
  runs-on: ubuntu-latest
  outputs:
    coverage: ${{ steps.coverage.outputs.percentage }}
```

**Add `id: coverage`** to the existing "Enforce coverage threshold" step and export the
value to `GITHUB_OUTPUT` **before** the threshold check (so it's written even if the step
later exits 1):
```yaml
- name: Enforce coverage threshold
  id: coverage
  run: |
    grep -v '/testutil/' coverage.out > coverage-prod.out
    TOTAL=$(go tool cover -func=coverage-prod.out | grep total | awk '{print $3}' | tr -d '%')
    echo "Unit test coverage: $TOTAL%"
    go tool cover -func=coverage-prod.out
    echo "percentage=$TOTAL" >> "$GITHUB_OUTPUT"
    THRESHOLD=70
    if [ "$(echo "$TOTAL < $THRESHOLD" | bc)" -eq 1 ]; then
      echo "::error::Coverage $TOTAL% is below minimum threshold of $THRESHOLD%"
      exit 1
    fi
```

### 2. Merge-to-main job — remove Go entirely, use job output

**Remove** the `actions/setup-go@v5` step (lines 200–203).

**Remove** the "Extract unit test coverage" step (lines 204–212).

**Update** the "Update coverage badge" step to reference `needs.go.outputs.coverage`
instead of the now-gone `steps.coverage.outputs.percentage`:
```yaml
- name: Update coverage badge
  uses: schneegans/dynamic-badges-action@v1.7.0
  with:
    auth: ${{ secrets.GIST_SECRET }}
    gistID: cf505c74b56eab52c9497af517b53222
    filename: opengate-coverage.json
    label: Go Server Coverage
    message: ${{ needs.go.outputs.coverage }}%
    valColorRange: ${{ needs.go.outputs.coverage }}
    minColorRange: 75
    maxColorRange: 85
```

`needs.go.outputs.coverage` is available because `merge-to-main` already lists `go` in
its `needs:` (line 181).

## File to modify
`.github/workflows/ci.yml` — go job and merge-to-main job only.

## Verification
Push to `dev` → merge-to-main job completes → coverage badge updates.
The "Extract unit test coverage" step should be gone from the merge-to-main log;
the badge step should show the same percentage that was logged in the go job.
