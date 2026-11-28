# Nightly Review — Every Session Reads the Timed Runs First

**Enforced by:** [`.claude/hooks/session-start-nightly.sh`](../hooks/session-start-nightly.sh)
(the list), the hold in [`pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh),
[`pretooluse-bash-source-write-guard.sh`](../hooks/pretooluse-bash-source-write-guard.sh) and
[`pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh), over
[`scripts/nightly-review.sh`](../../scripts/nightly-review.sh). **No bypass.** Why:
[ADR-123](../../docs/adr/ADR-123-alert-delivery.md).

The procedure is the [`nightly-review`](../skills/nightly-review/SKILL.md) skill.

## The rules

### The review comes before any change

- Every session starts with a review of the timed workflows' runs not yet reviewed, and of the
  timed workflows whose run never came.
- Until each has a written outcome, no file in the project changes and nothing is committed.
  Reading, searching and talking with Ivan stay open, and so do files outside the project: memory
  and the review's own write-up.

### A reported regression is a regression until shown otherwise

- The review looks for evidence that it is real, not for reasons that it is not.
- A failure a plan step already covers is shown to be that same failure: the same step, the same
  error text.

### Every item gets one of four outcomes

| Outcome | Its evidence |
|---|---|
| `regression` | what broke, and the plan step or fast-lane commit that fixes it |
| `broken-test` | why the test, not the product, is wrong |
| `did-its-job` | that its work actually ran: readings pushed, reports uploaded, alerts sent |
| `not-found` | everything checked, and why the reported regression did not show; it goes to Ivan in a modal |

## What is due

- [`nightly-review.sh fetch`](../../scripts/nightly-review.sh) makes one GitHub call and writes
  `.claude/.markers/nightly.due`: each finished timed run newer than the newest reviewed run of its
  workflow, and each timed workflow with no run started within its period plus 12 hours.
- The first review of a workflow starts at its newest finished run. A run still going waits for the
  next session; a run started by hand is reviewed by whoever started it.
- A fetch that cannot reach GitHub writes `unreachable`, and a missing list holds the files just as
  an unanswered one does.
- `nightly-review.sh done <write-up>` refuses a write-up missing an item or giving one no known
  outcome, and on success records the items and empties the list.
