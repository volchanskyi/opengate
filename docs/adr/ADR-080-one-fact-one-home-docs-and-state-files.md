---
adr: 080
title: "One Fact, One Home: the Three Doc Trees and the State-File Caps"
status: Accepted
date: 2026-08-19
---

# ADR-080: One Fact, One Home — the Three Doc Trees and the State-File Caps

## Status

Accepted.

## Context

`docs/` was organised by engineering concern, so every product capability was
described inside a build-or-run chapter. A reader asking what OpenGate tells a
technician when a customer's file server starts thrashing had to read a chapter
titled *Monitoring & Observability* that opened with Grafana port-forwards.
`Monitoring.md` was 1097 lines, roughly 660 of them product surface and roughly
440 the observability stack; `Architecture.md`, `Wire-Protocol.md` and
`Database.md` carried the same hybrid. There was no product documentation set at
all.

Three defects compounded. **Mixing**, above. **Duplication**: the same fact had
two homes and drifted in one — the vitals dimension vocabulary, the four rule
coverage states, the incident model, the tenancy model. And **past-state prose**:
[`docs-live-state.md`](../../.claude/rules/docs-live-state.md) forbade narrating
removed things, but nothing enforced it and it was re-violated repeatedly.

The same defect was larger outside `docs/`. `decisions.md` called itself a
compact index in its own header and had grown to 87 KB across 74 rows, a median
row of 783 characters. Measured by distinctive-term overlap, **85% of a row's
terms already appeared in the ADR it pointed at** — while verbatim overlap was
only 5%. That is the worse case, not the better one: the rows were *paraphrases*,
so the two copies drifted independently and no diff ever showed it. `phases.md`
was 235 KB across 159 rows, sharing 59% of its terms with the archived plan each
row linked. Together the three state files were 344 KB against a 772 KB `docs/`
tree.

## Decision

**A fact has one home, and everything else links to it.**

**Three trees.** `docs/product/` is what the system does, `docs/architecture/` is
how it is built, `docs/infrastructure/` is how it runs. One test decides which
tree a paragraph belongs in: *does it describe something a technician or a
customer can see or do, or does it describe how we build, deploy and run it?* A
two-way split would force the wire protocol and the schema in beside Terraform
and CI, where they do not belong — they are the product's implementation, not the
ground it runs on.

`README.md`, `Home.md` and the combined ADR log stay at the `docs/` root. The log
is the ADR corpus, not a chapter; relocating it is a separate decision about ADR
numbering and this split does not smuggle it in.

**The seam is a gate, not a convention.**
[`docs-seam.test.sh`](../../scripts/tests/docs-seam.test.sh) requires every
chapter to live in exactly one tree and to appear in exactly one `Home.md` row,
and refuses a product chapter that links a `deploy/`, `.github/`, `Makefile` or
`scripts/` path — mechanism leaking back across the seam.

**Live state is a gate too.**
[`docs-live-state.test.sh`](../../scripts/tests/docs-live-state.test.sh) matches a
measured phrase list against **paragraph-joined** text, because Markdown wraps at
~80 columns and a line-based grep misses every phrase that straddles a wrap. Its
scope is `docs/**` minus `docs/adr/**` and the combined log: an ADR's Context
section is *required* to state the problem the decision solved, so past-state is
structural there — the same document-class boundary
[`check-doc-links`](../../scripts/check-doc-links/) already draws around
`.claude/plans/**`. The phrase list is deliberately narrower than the rule's
prose: `used to `, `the old ` and `the previous ` each match ordinary live
writing, and a gate that fires on those is a gate somebody adds an allowlist to.
There is no allowlist file.

**The ADR is the only home of a decision and its why.** `decisions.md` is an
index, `phases.md` is a ledger, `techdebt.md` is a register of what is still
owed. Each row is a pointer with just enough text to choose a link.
[`state-index-density.test.sh`](../../scripts/tests/state-index-density.test.sh)
caps a `decisions.md` row at 200 characters of prose and a `phases.md` row at
300, measured with links, ADR numbers and table scaffolding removed. It also
checks the index is complete in both directions — every ADR file has exactly one
row, every row resolves to a file — which nothing checked before.

**Shorten by moving, never by deleting.** Before any row was cut its distinctive
terms were diffed against the ADR it points at, and anything substantive the ADR
lacked was written into the ADR first.

**The root `README.md` sells; `docs/` explains.** No protocol name, library name,
algorithm name, schema detail or stack table on the front page. A reader arriving
at the repo is deciding whether the product is interesting, not how it is built,
and every mechanism the page recited already has a home in `docs/`.

## Consequences

- A capability is described once, in a chapter named after it, and the
  build-or-run chapters link rather than restate. The vitals vocabulary, the four
  coverage states, the incident model and the tenancy model each have exactly one
  home.
- The three state files fall from 344 KB to roughly 90 KB, and a row that
  genuinely cannot say what shipped inside its cap is a signal that it is
  describing a decision and belongs in an ADR.
- Caps are a number a test enforces rather than guidance nobody can fail, which
  is the only version of this rule that survives a busy week.
- The product tree carries no diagram. All six rows of the coverage standard in
  [`docs/README.md`](../README.md) are architecture or infrastructure by nature,
  so this is expected rather than a gap.
- A doc-structure change now costs a gate run: adding a chapter means adding its
  `Home.md` row in the same commit, and a product chapter cannot quietly acquire
  a deploy link.
