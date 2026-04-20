# Local SonarCloud Analysis

## Context

SonarCloud quality gate failures on `dev` are caught only after pushing, wasting CI cycles and requiring fix-up commits. The current CLAUDE.md even states "SonarCloud has no local runner in this repo." This plan adds a local scan capability using the same SonarCloud backend as CI, so issues (code smells, bugs, security hotspots, coverage gates) are caught before code leaves the developer's machine.

## Approach

**Docker-based `sonar-scanner-cli` talking to SonarCloud** — identical rules, identical quality gate, no local SonarQube infrastructure. The scanner runs inside the `sonarsource/sonar-scanner-cli` Docker image, reusing the existing `sonar-project.properties`. A `SONAR_TOKEN` in `.env` (already gitignored) authenticates.

Two Makefile targets + integration into the `/precommit` skill.

---

## Implementation Steps

### 1. Add Makefile targets

**File:** [Makefile](../../Makefile)

Add to `.PHONY` line: `sonar sonar-coverage sonar-quick`

**`sonar-coverage`** — generates all three coverage files matching `sonar-project.properties` paths:
```makefile
sonar-coverage:
	cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...
	cd agent && cargo llvm-cov nextest --workspace --lcov --output-path lcov.info \
		--ignore-filename-regex '(main\.rs|/webrtc\.rs|/terminal\.rs|/session/mod\.rs|/session/relay\.rs|/tests/)'
	cd web && npx vitest run --coverage
```

**`sonar`** — full scan with coverage (mirrors CI exactly):
```makefile
sonar: sonar-coverage
	@test -n "$$SONAR_TOKEN" || (test -f .env && . ./.env && test -n "$$SONAR_TOKEN") || \
		{ echo "ERROR: SONAR_TOKEN not set. Export it or add to .env"; exit 1; }
	docker run --rm \
		-e SONAR_TOKEN=$${SONAR_TOKEN} \
		-v "$$(pwd):/usr/src" \
		-w /usr/src \
		sonarsource/sonar-scanner-cli:latest \
		-Dsonar.qualitygate.wait=true \
		-Dsonar.branch.name=dev
```

**`sonar-quick`** — code-quality-only scan, skips coverage generation (faster):
```makefile
sonar-quick:
	@test -n "$$SONAR_TOKEN" || (test -f .env && . ./.env && test -n "$$SONAR_TOKEN") || \
		{ echo "ERROR: SONAR_TOKEN not set. Export it or add to .env"; exit 1; }
	docker run --rm \
		-e SONAR_TOKEN=$${SONAR_TOKEN} \
		-v "$$(pwd):/usr/src" \
		-w /usr/src \
		sonarsource/sonar-scanner-cli:latest \
		-Dsonar.qualitygate.wait=true \
		-Dsonar.branch.name=dev
```

### 2. Token management

**File:** `.env` (developer creates locally, already in `.gitignore`)

```
SONAR_TOKEN=sqp_xxxxx
```

Token is generated at `sonarcloud.io/account/security` — a User Token scoped to `volchanskyi` org.

The Makefile targets check `$SONAR_TOKEN` from environment first, then fall back to sourcing `.env`. No secrets committed.

### 3. Integrate into `/precommit` skill

**File:** [.claude/skills/precommit/SKILL.md](../../.claude/skills/precommit/SKILL.md)

Add a new section **after** the Coverage checks (step 14) and **before** Benchmarks (step 15):

```markdown
## SonarCloud local scan (run if SONAR_TOKEN available)

15. `make sonar-quick` — Run SonarCloud analysis locally via Docker. Catches code smells, bugs, 
    security hotspots, and duplication that CI would flag. Requires Docker running and 
    `SONAR_TOKEN` set (in environment or `.env`). **Skip if token not configured** — 
    this step is best-effort since not all environments will have the token.
```

Renumber subsequent steps (benchmarks become 16-17, docs become 18-19).

Update Gate Criteria to include: "SonarCloud quality gate fails (if token configured)"

### 4. Update CLAUDE.md

**File:** [CLAUDE.md](../../CLAUDE.md)

- **SonarCloud Workflow section** (~line 75): Remove "SonarCloud has no local runner in this repo" and replace with guidance to run `make sonar` or `make sonar-quick` before pushing.
- **Commands section** (~line 100): Add:
  - `make sonar` — full local SonarCloud scan (generates coverage + runs scanner)
  - `make sonar-quick` — code-quality-only SonarCloud scan (no coverage generation)
  - `make sonar-coverage` — generate all coverage files for SonarCloud

### 5. Update docs

**File:** [docs/CI-Pipeline.md](../../docs/CI-Pipeline.md) (or appropriate docs page)

Add a "Local SonarCloud Analysis" section documenting:
- Prerequisites: Docker, `SONAR_TOKEN` in `.env`
- How to get a token
- `make sonar` vs `make sonar-quick` usage
- What the scan covers vs what it doesn't (e.g., PR decoration only works in CI)

---

## Files Modified

| File | Change |
|------|--------|
| `Makefile` | Add `sonar`, `sonar-coverage`, `sonar-quick` targets + `.PHONY` update |
| `.claude/skills/precommit/SKILL.md` | Add SonarCloud scan step, renumber subsequent steps |
| `CLAUDE.md` | Update SonarCloud section + Commands section |
| `docs/CI-Pipeline.md` | Add local analysis documentation |

No new files created (`.env` is developer-created).

---

## Verification

1. **Token setup:** Create `.env` with `SONAR_TOKEN=<real-token>`, confirm `echo $SONAR_TOKEN` works
2. **Quick scan:** `make sonar-quick` — should pull Docker image, run scanner, report quality gate status
3. **Full scan:** `make sonar` — should generate all 3 coverage files then run scanner
4. **Missing token:** Unset `SONAR_TOKEN` and remove `.env`, run `make sonar` — should fail with clear error message
5. **CI parity:** Compare local scan results on SonarCloud dashboard with latest CI run — rules and findings should match
