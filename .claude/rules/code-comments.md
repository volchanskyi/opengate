# Code Comments — Short Statements About The Code They Sit On

**Enforced by:**
[`.claude/hooks/pretooluse-comment-check.sh`](../hooks/pretooluse-comment-check.sh)
(write time) and
[`scripts/tests/check-comments.test.sh`](../../scripts/tests/check-comments.test.sh)
(gauntlet shell-tests step and CI `config-lint`), both over
[`scripts/check-comments/`](../../scripts/check-comments/). **No bypass.**

Companion to [`docs-live-state.md`](docs-live-state.md), which governs `docs/`.
This governs every comment in code, tests, scripts, hooks, workflows, deploy,
infrastructure, policy, migrations and configuration.

## The standard

A comment:

1. States what the code does, or why it has this shape where the code cannot
   show it: a constraint, invariant, unit, ordering, platform or protocol quirk,
   security or tenant boundary, or edge case.
2. Is a declarative sentence in the present tense and third person.
3. Is at most two lines, each at most 100 characters.
4. Is positive. "Never" or "must not" stands only where the prohibition is the
   safety rule itself.
5. Describes the code it sits on.

Code that needs no explanation carries no comment. None is added where none
was needed.

## What the checker refuses

| Code | Refused | Allowed |
|---|---|---|
| `length` | an own-line block over two lines | `Usage:`, `Environment:` and `Exit codes:` lists and the Sonar `JUSTIFICATIONS` list, one line per entry; directives |
| `width` | a comment line over 100 characters | directives |
| `doc-ref` | `ADR`, `ADR-123`, `WS-19`, `Phase B`, Markdown file names, `§` | rule files under `.claude/rules/`; `RFC 9000 §8.1` and other standard numbers; a path the same file's code names |
| `link` | URLs, `PR 12`, `#123`, `run 26929821908`, `commit 2acbdbdc`, a bare commit hash | example hosts (`example.com`, `.invalid`, `.test`, `localhost`); a URL or hash the same file's code names |
| `date` | `2026-09-13` | a timestamp with a time part |
| `history` | previously, formerly, historically, no longer, until now, last checked, kept for rollback, dormant | |
| `negation` | is/are not a/an/the, isn't, aren't, rather than, instead of, unlike, not only | |
| `person` | we, we're, we've, we'd, our, ours | |
| `divider` | four or more of `- = # * ─` in a row | |
| `test-doc` | a comment on or directly above a Go `Test*`, `Benchmark*` or `Fuzz*` function, a Rust `#[test]` function, or a TypeScript `it(`, `test(` or `describe(` (and `.each`) | directives |

Permanent standard numbers stay: `RFC 9000 §8.1`, `CVE-…`, `GHSA-…`,
`RUSTSEC-…`, and a fact about an outside system on the line that handles it.

## What the reviewer applies

- No narration, no restating the code, no commented-out code, no questions and
  no second person.
- Every statement is true of the code beside it.
- Required API documentation — Go exported types, Rust public items — is one
  statement that adds to the name. A Go doc comment starts with the identifier.
- A test's name states its behaviour. A comment inside a test sits only beside
  setup that encodes a condition the code cannot show.

## Kept verbatim

- Directives: the shebang, `# shellcheck`, `//go:` lines,
  `// Code generated … DO NOT EDIT.`, `// #nosec`, `// @ts-expect-error`,
  `// @ts-nocheck`, `/// <reference`, `// @vitest-environment`,
  `/** @type … */`, `# hadolint`, Dockerfile parser directives, and the
  `# vX.Y.Z` after a pinned action hash.
- Contract markers a gate reads: `(required)` in `Environment:` lists and
  `Not retried` beside cluster calls (read by
  [`ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh)),
  and the `JUSTIFICATIONS` list (read by
  [`sonar-coverage-exclusion-guard.sh`](../../scripts/sonar-coverage-exclusion-guard.sh)).

## How the checker reads

- One extractor per language, each exact: `go/scanner` for Go, a lexer for
  Rust, the TypeScript compiler in `web/node_modules` for TypeScript,
  JavaScript and JSON, `shfmt`'s syntax tree for shell, and line rules for
  YAML, Helm templates, Makefile, Dockerfile, TOML, HCL, SQL, Rego, Python,
  properties, CSS, HTML and the ignore files. A GitHub `run:` block is read as
  shell; any other block scalar, an HCL heredoc and an SQL dollar-quoted body
  are data.
- Every tracked file is either read or listed in
  [`scope.tsv`](../../scripts/check-comments/scope.tsv) with its reason. A file
  of an unknown type fails the sweep, and so does a scope entry that matches no
  file.
- The hook refuses only a violation the edit adds; one already in the file is
  left for the sweep.
- A missing TypeScript compiler or a drifted `shfmt` exits 2 with the command
  that fixes it.

## Modes

| Mode | What it does |
|---|---|
| `sweep [PATH…]` | Every in-scope file, zero violations; prints what it read and fails on a read of nothing |
| `hook` | Applies the proposed Write, Edit or MultiEdit in memory and refuses any new violation |
| `same-code BASE [--no-new-comments] [--declared FILE]` | Proves each changed file's code identical to `BASE` and, with the flag, that no comment sits where `BASE` had none |
| `stats [PATH…]` | Comment lines per language and area |
