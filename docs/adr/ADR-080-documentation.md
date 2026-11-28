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
The live-state gate
([`docs-live-state.test.sh`](../../scripts/tests/docs-live-state.test.sh))
matches paragraph-joined text, so a phrase straddling an 80-column wrap is
caught, and carries no allowlist. Its phrase list is narrower than the rule:
`used to `, `the old ` and `the previous ` are left out, since each also reads
as plain description — "the namespace used to construct the endpoint" — in a
dozen live pages. Its scope is `docs/**` minus the decision records, whose
Context sections state the problem a decision solved.

**Every page opens with its contents.** Each page under `docs/` except the
decision records starts, right after its title, with a list linking every
`##` to `####` heading in order, and its headings are short, plain and unique on
the page. A long page is navigated rather than read top to bottom, and the list
is what a reader navigates by; one written by hand falls behind the first heading
anybody adds, so the link check holds the list to the headings with the anchors it
already resolves, and a renamed heading has its links repointed in the same
change. Decision records are read by number rather than navigated, so they are
left out.

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

### How a decision is reached before work starts

**A question to Ivan shows its research.** On 2026-11-26 alone Ivan sent four decision
questions back: one offered options built without research, one gave the research's
own cost where the code's cost on speed and upkeep belonged, one named a product
without saying what it is, and one leaned on terms of art. Notes to the session held
the rule already and did not hold it in practice, so two checks hold it now
([`decision-questions.md`](../../.claude/rules/decision-questions.md)). A guard reads the
session log and refuses a question unless, since Ivan's last typed message, the session
searched the web on how others do it and searched git and GitHub Actions history for an
earlier attempt — the way a reviewed pull request's approval counts only for the code it
saw. A second model then reads the question before it opens and refuses one whose words
are jargon, whose outside products go unexplained, whose options lack their good, their
bad, how a named product builds them, their effect on speed and the upkeep of their code
(the form MADR's "good, because / bad, because" gives every option), or whose tests do
not say what they catch. That second check is a model and not a word list because plain
words, an explained product and an option's full account are judgments; Claude Code's
own hooks guide suggests a model-run check for exactly such calls. Its reason comes back
to the session, which mends the question and asks again.

**A plan starts with nothing open, and fixes ride it.** Of the 37 commits between
2026-10-26 and 2026-11-26, 22 were fixes, each costing a 40-43 minute gauntlet run and a
10-17 minute CI run, and two plans carried questions to Ivan that nothing stopped them
starting with. So every plan states its status on the line under its title, and a plan is
set active only with nothing open in it — the Definition of Ready Microsoft, GitLab and
Scrum teams agree before work starts, and stricter than Rust's RFCs, which list the points
left to settle during the work. A guard refuses a plan set active beside a question, an
open-question heading or an open word, and an edit of an active plan in pieces, since an
active plan is rewritten whole; a second model reads the whole plan for a choice left
unmade or a step that does not say what to do. While a plan is active, a fix found along
the way joins its next commit after Ivan's yes — the release train Firefox runs, and the
grouping Dependabot gives this project's updates. A fix for production down or deploys
failing takes the fast lane and its own commit at once, as Firefox's uplift does. Every
commit message ends in a `Plan:` line the commit guard reads: an active plan, `none` while
no plan is active, or `fast lane` while the newest deploy failed or production's health
address answers anything but 200
([`plans-and-adrs.md`](../../.claude/rules/plans-and-adrs.md)).

## Consequences

An ADR that nobody can edit rots, and a reader cannot tell a rotted ADR from a
current one. Mutability costs the ability to read a decision as it was
originally written, which git provides.

A model's answers vary from run to run, so neither model check carries a test: a wrong
refusal shows at once on the next question or plan, and a wrong pass is no worse than no
check. The searches a question needs are read from Claude Code's session log, whose shape
a client update can change; the guard refuses when it cannot read the log, so that change
shows as a refusal on the first question.
