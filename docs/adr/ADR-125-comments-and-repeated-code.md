---
number: 125
title: A comment is a short statement about its code, and repeated code is read across the whole codebase
---

# ADR-125 — A comment is a short statement about its code, and repeated code is read across the whole codebase

## Context

Comments had grown to nearly a fifth of the non-blank lines in the repository.
Thousands of blocks ran six lines or more, and they told stories: the night a
gate failed, an example machine that locked up, the run that showed it, the
decision record that explains it, the plan phase that added it. Those
references move under the code — a decision record is rewritten, a plan is
deleted, a run expires — so a comment that leans on one goes stale without the
code changing at all. Nothing read a comment: the live-state gate covered
`docs/` only.

Repeated code had the same blind spot from the other side. The gate's
duplication condition counts new code only, attributed by git blame, so the
local guard asked SonarCloud about the files a commit touched. A file copied
months ago, and every file nobody touched since, sat outside both.

## Decision

**A comment states what the code beside it does, or why it has this shape
where the code cannot show it.** It is a declarative sentence, positive, at most
two lines of at most 100 characters, and it names nothing outside the
repository that can move: no decision record, plan, phase, link, ticket, run,
commit or date. A test's name states its behaviour, so nothing sits on or above
a test. A comment the code makes redundant is deleted rather than rewritten.
The full standard is
[`code-comments.md`](../../.claude/rules/code-comments.md).

**A checker holds the standard at two points.** At write time a hook refuses an
edit that adds a violation; on every commit the gauntlet sweeps every tracked
file, and CI's configuration job runs the same sweep. Each language has an
exact extractor — the compiler's own scanner or parser where one exists, a
syntax tree for shell, line rules for configuration — so a string that looks
like a comment is never read as one. Every tracked file is either read or named
out of scope with its reason, and a file of a type nothing reads fails the
sweep, so a new language arriving with a new platform is a decision rather than
a gap.

**Machine-read comments stay verbatim.** Compiler and linter directives, the
version beside a pinned action, and the markers other gates read are exempt
from the rules and checked by those gates.

**Repeated code is read from the scanner's own result, for every file.** The
scanner computes repeated code on the machine that runs it and writes it into
its report. `make sonar` and the CI scan keep that report, and a reader
computes each production file's share of repeated lines exactly as Sonar does —
the union of its repeated lines over its length — and refuses any file above
3%. A report that is missing, unreadable, or names no production file is
refused, so a change in Sonar's format fails loudly. The scanner itself is
pinned in the tool-version manifest; a real report committed as a fixture pins
the layout the reader expects.

## Consequences

- Comment lines fell by more than two thirds. What remains is the reason a
  reader needs: a tenant boundary, a protocol quirk, a unit, an ordering.
- Removing comments lowers PMAT's grade of a Go file, whose comment term is
  proportional to comment lines. Files that dropped below B+ were lifted by
  improving their code — splitting by concern, removing repetition — never by
  keeping comments.
- The duplication gate now covers the whole codebase on every commit, so a
  repeated block anywhere fails the commit that next runs the gauntlet, not
  only the commit that wrote it.
- Every comment-only change is proved identical in code by the checker's
  `same-code` mode: token streams for Go, Rust and TypeScript, the syntax tree
  for shell, and comment-free lines for the rest.
