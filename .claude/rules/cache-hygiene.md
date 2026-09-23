# Local Cache Hygiene

**Enforced by:** [`.claude/hooks/post-push-clean-caches.sh`](../hooks/post-push-clean-caches.sh)
(the reclaim itself), chained from
[`.claude/hooks/git-post-commit.sh`](../hooks/git-post-commit.sh) after a
successful auto-push and from
[`.claude/hooks/posttooluse-cache-clean.sh`](../hooks/posttooluse-cache-clean.sh)
after any other push. **No bypass.**

Reclaim the local build caches after **every** push. No exceptions.

## What gets cleared

- See [`post-push-clean-caches.sh`](../hooks/post-push-clean-caches.sh) for the
  exact commands.
- `docker volume prune` alone is not enough. The builder cache is co-equal with
  `agent/target` as the largest consumer, and only `docker builder prune`
  reclaims it.
- Everything cleared is a rebuild, never a re-download.

## Never cleared

The cargo registry, the Go module cache, and tagged Docker images.
`docker image prune -a` and `docker system prune -a` are banned; bare
`image prune -f` drops dangling layers only.

## Two triggers

1. **Every push.** The auto-push path cleans on its way out. Any other push — a
   manual push tool call, or a retry after the auto-push aborted on a rebase
   conflict — is caught by the PostToolUse janitor.
2. **A free-space floor.** Below the floor the cleaner runs on any Bash call,
   push or not. The floor defaults to 40 GiB; `OPENGATE_DISK_FLOOR_GB`
   overrides it.

Both triggers defer while a build is in flight — `cargo clean` against a live
target directory corrupts the build it races. The next Bash call after the build
finishes still sees the disk below the floor and reclaims then.

Run it by hand any time with `.claude/hooks/post-push-clean-caches.sh`.

## Checking

`df -h /` reports free space; `docker system df` breaks down what Docker holds
and how much is reclaimable.
