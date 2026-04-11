---
name: wiki-audit
description: |
  Audit /docs for drift from the source of truth. Greps the docs tree for
  drift-prone patterns (percentages, version pins, file paths, config flags,
  port numbers, CLI flags) and verifies each finding against the code/config
  it references. Reports stale facts and fixes them in-place where the fix
  is obvious; flags ambiguous cases for human review.
---

# Wiki Audit

**Purpose.** Prose documentation drifts from code whenever a fact is
*paraphrased* (copied as a number or identifier) instead of *linked* (pointing
to the file where the fact actually lives). This skill systematically hunts
for drift and either fixes it or rewrites the passage to use a link, per the
convention in [`docs/README.md`](../../../docs/README.md).

**Scope.** `docs/**/*.md` and the root `README.md`. `docs/adr/*.md` files are
**immutable** — if an ADR is stale, the fix is a new superseding ADR, never an
in-place edit. Flag, do not edit.

**Reference:** [`docs/README.md`](../../../docs/README.md) — link-over-paraphrase rule.

---

## 1. Drift-prone patterns

For each pattern, grep `docs/` and `README.md`, then verify each hit against
the source of truth listed. When a fact is stated as a bare number or literal
in prose, the correct fix is almost always to **replace it with a link** to
the source — not to update the number.

### 1a. Percentages

```bash
grep -rn '[0-9]\+%' docs/ README.md
```

Every percentage is suspect. Common drift cases:
- Coverage thresholds — verify against `-Dsonar.qualitygate.wait` conditions
  in SonarCloud UI and against `--fail-under-lines`, `go tool cover`, and
  `coverage-summary.json` checks in [`.github/workflows/ci.yml`](../../../.github/workflows/ci.yml).
- Duplication limits — verify against SonarCloud UI.
- SLA / availability numbers — verify against deploy config.

**Fix pattern:** replace `≥ 80%` with a link to the line in `ci.yml` that
enforces it. Leave the number only if it appears adjacent to the link in a
summary table.

### 1b. Version pins

```bash
grep -rEn '(go|rust|node|npm|python|tsc) ?[0-9]+\.[0-9]+' docs/ README.md
grep -rEn '@[0-9]+\.[0-9]+\.[0-9]+' docs/ README.md
grep -rEn 'v[0-9]+\.[0-9]+\.[0-9]+' docs/ README.md
```

Verify each version against:
- Go: [`server/go.mod`](../../../server/go.mod) `go` directive
- Rust: [`rust-toolchain.toml`](../../../rust-toolchain.toml) if present, else
  `Cargo.toml` `rust-version`
- Node: `.nvmrc` or `package.json` `engines`
- GitHub Actions: `uses:` lines in [`.github/workflows/`](../../../.github/workflows/)
- Docker base images: `FROM` lines in [`Dockerfile`](../../../Dockerfile) and
  [`deploy/`](../../../deploy/)

**Fix pattern:** replace pinned versions in prose with a link to the pin file.

### 1c. File paths mentioned in prose

```bash
grep -rEn '`[a-zA-Z_./-]+\.(go|rs|ts|tsx|yaml|yml|toml|md|sh)`' docs/ README.md
grep -rEn '`[a-zA-Z_][a-zA-Z0-9_/.-]+/[a-zA-Z0-9_.-]+`' docs/ README.md
```

For each referenced path, verify the file still exists and the function/type
named alongside it still exists at that path.

```bash
# Verify each path exists
while read -r path; do
  [ -e "$path" ] || echo "MISSING: $path"
done < <(grep -rhoE '`[a-zA-Z_][a-zA-Z0-9_/.-]+\.(go|rs|ts|tsx|yaml|yml|toml)`' docs/ README.md | tr -d '`')
```

**Fix pattern:** paths should be markdown links (`[text](relative/path)`)
rather than backtick-quoted strings. Links at least make relocation visible
(the link breaks) instead of silent.

### 1d. Config flags and CLI options

```bash
grep -rEn '`-[-a-z]+`' docs/ README.md
grep -rEn '\-\-[a-z][a-z-]+' docs/ README.md
```

Verify each flag against its source:
- Go binaries: `flag.String`/`flag.Int` calls in `server/cmd/*/main.go`
- Rust binaries: `clap` derives and `Arg::new` calls in `agent/crates/*/src/main.rs`
- CI: `uses:` action inputs
- Cargo / npm scripts: workspace `Cargo.toml` / `package.json`

**Fix pattern:** replace flag enumerations in prose with a link to the source
and a short semantic description ("auth flags" → link to middleware).

### 1e. Port numbers

```bash
grep -rEn ':[0-9]{2,5}\b' docs/ README.md | grep -vE 'https?://|ADR-|\.md:'
```

Verify ports against default flag values in `main.go` / `main.rs` and against
`docker-compose*.yml` port mappings.

### 1f. Environment variable names

```bash
grep -rEn '\$?[A-Z][A-Z0-9_]{3,}=' docs/ README.md
grep -rEn '`[A-Z][A-Z0-9_]{3,}`' docs/ README.md | grep -v 'ADR-'
```

Verify every uppercase env var against:
- `os.Getenv` / `LookupEnv` in Go
- `std::env::var` in Rust
- `import.meta.env` in Vite
- `.env.example` files in deploy

**Fix pattern:** env vars should be listed once in an authoritative location
(the binary that reads them) and linked from anywhere else.

### 1g. SonarCloud / quality gate claims

```bash
grep -rni 'sonar\|quality.gate\|sarif\|code scanning\|clean.as.you.code' docs/ README.md
```

Verify each claim against:
- [`.github/workflows/ci.yml`](../../../.github/workflows/ci.yml) — scan
  action config
- [`sonar-project.properties`](../../../sonar-project.properties)
- SonarCloud project settings (Quality Gate tab)

Historical drift: SARIF upload to GitHub Code Scanning was removed in
commit 9236826 but wiki text mentioning it persisted for weeks. Grep for
`sarif` and `code scanning` and verify the scan action actually produces
SARIF output.

### 1h. Wire protocol constants

```bash
grep -rEn '0x[0-9a-fA-F]{2}|\b[0-9]+-byte\b' docs/
```

Verify against:
- Frame format: [`agent/crates/mesh-protocol/src/frame.rs`](../../../agent/crates/mesh-protocol/src/frame.rs)
- Type bytes: same file, const declarations
- Golden fixtures: [`testdata/golden/`](../../../testdata/golden/)

### 1i. Broken markdown links

```bash
grep -rEn '\]\(\.\.?/[^)]+\)' docs/ README.md | while read -r line; do
  file="${line%%:*}"
  link=$(echo "$line" | grep -oE '\]\(\.\.?/[^)]+\)' | sed 's/^](\(.*\))$/\1/')
  dir=$(dirname "$file")
  target=$(realpath -m "$dir/$link" 2>/dev/null)
  [ -e "$target" ] || echo "BROKEN: $file -> $link"
done
```

---

## 2. ADR immutability check

ADRs in `docs/adr/` must never be edited after acceptance. Verify:

```bash
git log --follow --format='%h %s' -- docs/adr/*.md | head -50
```

Any commit that *modifies* an existing ADR file (as opposed to adding a new
one) is a violation. The fix is to revert the edit and write a superseding
ADR instead. The combined historical log at
[`docs/Architecture-Decision-Records.md`](../../../docs/Architecture-Decision-Records.md)
is also frozen — do not append to it; new ADRs go in `docs/adr/`.

---

## 3. Phase / techdebt consistency

```bash
grep -n 'Phase [0-9]' docs/ -r
```

Cross-reference phase claims against [`.claude/phases.md`](../../../.claude/phases.md).
If the docs describe a feature as "Phase 12" but `phases.md` lists it as
in-progress, flag the discrepancy.

---

## 4. Reporting format

After running each check, produce a table:

```
+-----+-----------------------------+-----------------+------------------------------+
| #   | Finding                     | File:line       | Fix                          |
+-----+-----------------------------+-----------------+------------------------------+
| 1   | "70% coverage threshold"    | CI-Pipeline.md  | Replaced with link to        |
|     |                             |   :157          | ci.yml `fail-under-lines`    |
| 2   | "SARIF upload" (removed)    | Architecture-   | Deleted section; noted       |
|     |                             |   Decision-...  | removal in commit 9236826    |
+-----+-----------------------------+-----------------+------------------------------+
```

Status values: **FIXED** (edited in place with the link-over-paraphrase
refactor), **FLAGGED** (ambiguous — requires human decision), **IMMUTABLE**
(ADR drift — cannot edit, must supersede).

---

## 5. Auto-fix rules

Only auto-fix in categories where the correct action is unambiguous:

| Category                 | Auto-fix?                                   |
|--------------------------|---------------------------------------------|
| Percentages              | Replace with link. Flag if source unclear.  |
| Version pins             | Replace with link to pin file.              |
| File paths               | Convert backticks to markdown links.        |
| Broken links             | Flag only — do not guess target.            |
| ADR content drift        | Flag only — ADRs are immutable.             |
| Phase/techdebt drift     | Flag only — requires human judgement.       |
| Env var drift            | Replace with link to the binary that reads. |

When unsure, **flag**. An unverified "fix" is drift in the opposite direction.

---

## 6. Gate criteria

The audit **PASSES** if there are zero FLAGGED findings after one pass of
auto-fixes. FIXED counts as passing (the drift is resolved). IMMUTABLE
findings must be resolved by writing a new ADR in the same session — they
block the gate otherwise.
