# OpenGate Documentation

Canonical developer documentation for the OpenGate remote device management
platform. All long-form docs live here, in the same git repo as the code they
describe. The previous GitHub wiki (`volchanskyi/opengate.wiki`) is deprecated.

Start at [Home.md](./Home.md) for the chapter index.

---

## Why docs live in the repo

Historically the wiki was a separate git repository. It drifted from the code
constantly: coverage thresholds changed in CI but not in the wiki, SARIF export was removed but the wiki still described it weeks
later,
ADR-012 kept accumulating in-place edits as the underlying policy shifted.

The root cause was structural: a PR that touched `ci.yml` never touched the
wiki repo, so there was nothing forcing the wiki change to happen in the same
review. Moving the docs into the same repo means:

- A PR that changes behaviour can be reviewed alongside the doc update.
- Code search (`ripgrep`, IDE find-in-files) finds doc references to renamed
  symbols.
- `CODEOWNERS` and path-based CI checks can warn when code paths change
  without docs/ being touched.
- The docs build/lint can run as part of the normal CI, not as a separate
  manual push.

---

## Documentation conventions

### 1. Link, don't paraphrase

If the source of truth for a fact lives in code or config, **link to it, do
not restate it in prose**. Any number, version pin, flag name, file path, or
port that you copy into docs will eventually drift from the source.

**Test for drift risk:** "If the underlying code changes, would I need to come
back and edit this sentence?" If yes, replace the sentence with a link.

**Examples — bad:**

> Rust coverage threshold is 80%, enforced by `cargo llvm-cov nextest
> --fail-under-lines 80` in CI.

> The server listens on port 8080 by default.

> Go version 1.23.6 is pinned in CI.

**Examples — good:**

> Rust coverage threshold is enforced in the `test-rust` job in
> [`ci.yml`](../.github/workflows/ci.yml) (search for `fail-under-lines`).

> The default HTTP listen address is the `-listen` flag default in
> [`cmd/meshserver/main.go`](../server/cmd/meshserver/main.go).

> CI runs against the Go version pinned in [`go.mod`](../server/go.mod).

When you must state a number inline (e.g. in a summary table), place it
adjacent to the link so a reader can one-click verify it.

### 2. Per-file ADRs are mutable; supersede only for decision *changes*

An Architecture Decision Record documents a decision. Per-file ADRs in
[`adr/`](./adr/) (ADR-013 onward) are **mutable** — edit them in place to keep
them accurate against current state: fix a rotted link, correct a moved path,
strip chronological noise from the body. git history (`git log --follow` per
file) is the audit trail.

Supersession is still used for genuine **decision changes** (a reversal or
replacement, not a correction): create a new ADR with the next number, set its
`supersedes:` frontmatter, and update the prior ADR's `status:`. The lineage
stays explicit so a reader asking "why was it X, and what changed?" can trace
`ADR-014 → supersedes ADR-003`. Mutability keeps an ADR *true*; supersession
records what *changed*.

ADR bodies follow the same current-state doctrine as the rest of the docs:
purge chronological/logistical noise, but **rewrite to preserve the fact and the
why — never delete substantive rationale**, and keep the
`date:`/`status:`/`supersedes:` frontmatter. An ADR may link a plan only under
`plans/archive/…` (active plans rot); fold other pointers inline or into the
[`index`](../.claude/decisions.md).

New ADRs live as individual files using the `ADR-NNN-kebab-title.md` naming
convention. The combined
[`Architecture-Decision-Records.md`](./Architecture-Decision-Records.md) is the
historical log from before the per-file regime and stays **frozen** — never
edited or appended to; mutability does not reach it. The compact
[`index`](../.claude/decisions.md) is updated for every new ADR. See
[`adr/ADR-036`](./adr/ADR-036-mutable-adrs-current-state-doctrine.md) for the
full doctrine.

### 3. No paraphrased ADR bodies

Do not copy ADR text into other pages. Other pages link to the ADR and
summarise only the one fact the reader needs in context. This prevents the
same fact from existing in two places and going stale in one.

### 4. Mermaid diagrams only

Use Mermaid fenced blocks for diagrams:

````markdown
```mermaid
flowchart LR
  A --> B
```
````

Do not commit rendered SVG blobs, D2 sources, or a diagram-rendering toolchain.
GitHub renders Mermaid server-side, so docs stay reviewable as plain Markdown
without Puppeteer, a JRE, or generated image drift. Keep diagrams hand-curated at
the architecture level; structural drift is caught by the boundary linters wired
through [`scripts/precommit-gauntlet.sh`](../scripts/precommit-gauntlet.sh) and
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml), not by auto-extracting
graphs from source.

---

## Directory layout

```
docs/
├── README.md                           This file
├── Home.md                             Chapter index
├── Architecture.md                     System overview
├── API-Reference.md                    REST API tour
├── Wire-Protocol.md                    Frame format, MessagePack, golden files
├── Platform-Abstraction.md             Agent platform traits
├── Database.md                         PostgreSQL schema, migrations, backups
├── Testing.md                          Test layers, tooling
├── Shell-Quality.md                    Bash policy, gates, behavioral tests
├── CI-Pipeline.md                      GitHub Actions pipeline
├── Continuous-Deployment.md            CD flow and rollback
├── Container-Images.md                 GHCR image build and signing
├── Monitoring.md                       VictoriaMetrics / Grafana / Loki
├── Infrastructure.md                   Terraform, OKE networking, bastion, secrets
├── Agent-Updates.md                    OTA update pipeline
├── Security-and-Dependencies.md        Vulnerability scanning, Dependabot
├── Architecture-Decision-Records.md    Frozen historical ADR log (ADR-001 … ADR-012)
├── adr/                                Per-file mutable ADRs (ADR-013+)
│   └── ADR-NNN-title.md
└── api/                                Generated Scalar OpenAPI reference
    └── index.html
```

---

## Keeping docs in sync with code

Two defences run continuously:

1. **`/wiki-audit` skill** (at [`.claude/skills/wiki-audit/`](../.claude/skills/wiki-audit/SKILL.md))
   greps the docs for drift-prone patterns (percentages, version pins, file
   paths, config flags, port numbers) and flags candidates for review. Run it
   before shipping a PR that changes any of those underlying values.

2. **Path-based CI warning.** When a PR touches `ci.yml`,
   `sonar-project.properties`, `Cargo.toml`, `go.mod`, `package.json`, or
   deploy configs without touching `docs/`, CI leaves a comment asking the
   author to confirm no doc update is needed. This is advisory, not blocking.
