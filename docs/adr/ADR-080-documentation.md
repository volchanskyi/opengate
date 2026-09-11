---
number: 80
title: How documentation and decisions are kept
---

# ADR-080 — How documentation and decisions are kept

## Context

The same fact lived in several places and the copies drifted. Measured on the
state files: 85% of an index row's distinctive terms already appeared in the ADR
it pointed at, but only 5% of the words matched, so the two said the same thing
differently and no diff ever showed them disagreeing.

## Decision

**Documentation lives in the repository, under [`docs/`](../).** It is versioned
with the code it describes and reviewed in the same change.

**A fact has one home and everything else links to it.** Numbers, versions,
flags and paths are not copied into prose. The test for a sentence: if the code
changed, would this sentence need editing? Then it should have been a link.

**Three trees, and one question decides which.** Can a technician or a customer
see or do this, or is it how we build, deploy and run it?
[`product/`](../product/) is the first, [`architecture/`](../architecture/) is
how it is built, [`infrastructure/`](../infrastructure/) is how it runs. A
chapter lives in exactly one tree, appears in exactly one row of
[`Home.md`](../Home.md), and a product chapter links no build path.

**Documentation describes live state only.** Nothing narrates what was removed
or replaced. Both the seam and this are gates, not conventions.

**Every ADR is editable and describes what is true now.** There is no superseded
status and no supersession chain: when a decision changes, the ADR that holds it
is rewritten, and if the old decision left nothing behind, it is deleted. What
changed is in git history, which is where a history belongs.

**Minor work does not get an ADR.** A fix, a patch or a repair belongs in the
ADR whose decision it refines, or nowhere.

**The ADR is the only home of a decision and its reasoning.** The state files
are pointers with just enough text to choose a link:
[`decisions.md`](../../.claude/decisions.md) is the index,
[`phases.md`](../../.claude/phases.md) is the ledger, and
[`techdebt.md`](../../.claude/techdebt.md) is the register. Index rows are
capped at 200 characters of prose and ledger rows at 300, and a row is shortened
by moving its content into the ADR, never by deleting it.

**The root README sells; `docs/` explains.** No protocol, library or file path
appears in the README.

## Consequences

An ADR that nobody can edit rots, and a reader cannot tell a rotted ADR from a
current one. Mutability costs the ability to read a decision as it was
originally written, which git provides.
