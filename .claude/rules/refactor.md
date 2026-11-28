# The order a commit takes: tidy-up, gauntlet, commit

**Enforced by:**
[`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh)
and git's own pre-commit check
[`.claude/hooks/git-pre-commit.sh`](../hooks/git-pre-commit.sh) (both records),
[`scripts/refactor-gate.sh`](../../scripts/refactor-gate.sh) (`/refactor`
start and finish), [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh)
(the pass), [`.claude/hooks/git-post-commit.sh`](../hooks/git-post-commit.sh)
and [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh)
(refactor marker), the write guards (no hand-written marker). **No bypass.** Why:
[ADR-132](../../docs/adr/ADR-132-a-pass-is-a-record-the-commit-asks-for.md).

## The order

1. Run `/refactor` on the change. It records the content it begins on, and
   what it left when it finishes.
2. Run [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) in
   the background, with no time limit, and poll it every 5 minutes. A full pass
   over content that did not change during the run writes `gauntlet.pass`; a
   failed or cut-short run writes nothing and clears the last one.
3. Commit, with a `Plan:` line closing the message
   ([`plans-and-adrs.md`](plans-and-adrs.md#every-commit-names-its-plan)). The
   commit guard and git's own pre-commit check both refuse unless `/refactor`
   finished on exactly the content on disk and a gauntlet pass names that content
   from the last hour. The check takes seconds.
4. The post-commit hook pushes, and marks the commit for the push guard.

Any edit after `/refactor` finishes starts the order over — a new file
included. The proof is about what is on disk, not about what is staged, so keep
the commit message file and any log of the run outside the work tree.

## The proof

Each step writes one marker in `.claude/.markers/` naming the content it ran on
([`tidy-up.sh`](../hooks/lib/tidy-up.sh)), and the next step refuses unless the
content in front of it is that content.

| Marker | Written by | Read by |
|---|---|---|
| `refactor.start` | `/refactor` start | `/refactor` finish, which spends it |
| `refactor.done` | `/refactor` finish | the commit guard, git's own pre-commit check, the post-commit hook |
| `gauntlet.pass` | a full gauntlet pass: the content and the second its run began | the commit guard, git's own pre-commit check, the post-commit hook |
| `refactor.head` | the post-commit hook; `/refactor` finish when nothing tracked is uncommitted | the push guard |

- Content is named by the tree of the working tree as it stands — tracked files
  and untracked files not ignored, read from disk — so staging does not move
  it and an edit or a new file does.
- A pass is good for one hour from its run's start, when the advisory lookups
  happen, while the content still matches.
- No marker is written by hand. The write guards refuse a `Write`/`Edit` into
  `.claude/.markers/` and a shell command that writes, copies, moves or removes
  a file there.

## The gauntlet

- [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) is the
  single source of truth for what a commit must pass: one full run per content,
  including docs-only and CI-only commits.
- A failed run costs a full run. Fix what the output names, run `/refactor`
  on the fix, and run it again.
- The commit guard and git's own check judge every repository that holds this
  project's first commit, the same way the push guard does ([Pushing](#pushing)),
  hand-made terminal commits included. A plain commit in another project's clone,
  such as an upstream fork, is left to that project.
- Exit 0 = every check passed and the pass is written. Exit 1 = one or more
  checks failed, or the content changed during the run. Exit 2 = a prerequisite
  is missing (`POSTGRES_TEST_URL`, `SONAR_TOKEN`, a reachable VictoriaMetrics, a
  `$HOME/go` shadow install). Fix the prerequisite and re-run; never bypass.
- The gauntlet does not assert documentation freshness. Update
  [`README.md`](../../README.md) and [`/docs`](../../docs/) pages the diff
  invalidates before committing.

### What the gauntlet runs

- [`precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) is the list: it runs its
  checks in order and prints each one's elapsed time.
- `PRECOMMIT_SKIP_BENCH=1` skips the benchmarks, only for an iteration that touches nothing
  performance-sensitive.
- The end-to-end tests and the SonarCloud scan run only once every earlier check passed. That
  changes runtime, never the pass/fail outcome.

### Why every commit

Lockfile audits (`cargo audit`, `govulncheck`, `npm audit`) gate on the current
advisory database, not the diff — an advisory published today fails a docs-only
commit tomorrow. SonarCloud, lints and e2e gate on full-repo state.

## Pushing

- The post-commit hook pushes a commit made from content both `refactor.done`
  and `gauntlet.pass` name, and writes `refactor.head` for it. A commit with no
  such proof — a terminal commit run with `-n` — is neither marked nor pushed.
- The push guard blocks a push whenever there are any commits since
  `origin/dev` unless `refactor.head` equals HEAD, regardless of what files they
  touch. There is no doc-only or CI-only exemption.
- The push guard judges every repository that holds this project's first
  commit: its clones and work trees. A plain push from another project's clone,
  such as an upstream fork, is left to that project. A push command that
  changes directory or repository (`cd`, `pushd`, `-C`, `--git-dir`,
  `--work-tree`, `GIT_DIR`, `GIT_WORK_TREE`) is judged wherever it starts.
- A commit rebased by hand, or made with no tidy-up behind it, is cleared the
  same way: `/refactor`. With nothing tracked left uncommitted, its finish marks
  HEAD.

## Multi-PR rollouts

- Every PR passes the gate, including docs-only PRs.
- Exception: a fast-follow commit fixing a CI failure already reported by a
  previous push.
