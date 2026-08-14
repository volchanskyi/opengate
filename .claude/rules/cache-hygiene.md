# Local Cache Hygiene

**Enforced by:** [`.claude/hooks/post-push-clean-caches.sh`](../hooks/post-push-clean-caches.sh)
(the reclaim itself), chained from
[`.claude/hooks/git-post-commit.sh`](../hooks/git-post-commit.sh) after a
successful auto-push and from
[`.claude/hooks/posttooluse-cache-clean.sh`](../hooks/posttooluse-cache-clean.sh)
after any other push. **No bypass.**

Reclaim the local build caches after **every** push. No exceptions — not for a
docs-only push, not for a one-line fix, not for "I'll do it after the next
change".

## Why

The caches are enormous and grow without bound: the Rust `agent/target` tree and
Docker's build cache each reach tens of GB, and every `make e2e` / gauntlet run
adds a fresh layer set. Left alone they fill the WSL disk and break local
development outright — the machine stops being able to build, test, or run
anything.

Nothing reclaimed is precious. Production runs on OKE, and every cleared item is
a **rebuild**, never a re-download.

## What gets cleared

See [`post-push-clean-caches.sh`](../hooks/post-push-clean-caches.sh) for the
exact commands. `docker volume prune` alone is not enough — the builder cache is
co-equal with `agent/target` as the largest consumer, and only
`docker builder prune` reclaims it.

Never cleared, because they force re-downloads rather than rebuilds: the cargo
registry, the Go module cache, and tagged Docker images (`docker image prune -a`
/ `docker system prune -a` are banned; bare `image prune -f` drops dangling
layers only).

## Two triggers, so a missed push cannot fill the disk

1. **Every push.** The auto-push path cleans on its way out. Any other push — a
   manual `git push` tool call, or a retry after the auto-push aborted on a
   rebase conflict — is caught by the PostToolUse janitor.
2. **A free-space floor.** Below the floor the cleaner runs on any Bash call,
   push or not, so a long stretch of work without a push still cannot exhaust
   the disk. The floor defaults to 40 GiB; `OPENGATE_DISK_FLOOR_GB` overrides it.

Both triggers defer while a build is in flight — the gauntlet is routinely run
backgrounded while other work continues, and `cargo clean` against a live target
directory corrupts the build it races. Deferring is free: the next Bash call
after the build finishes still sees the disk below the floor and reclaims then.

Run it by hand any time with
`.claude/hooks/post-push-clean-caches.sh`.

## Checking

`df -h /` reports free space; `docker system df` breaks down what Docker holds
and how much of it is reclaimable.
