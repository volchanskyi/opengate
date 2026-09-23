# Assertion Determinism — An Assertion Survives Its Own Pipe

**Enforced by:**
[`scripts/tests/pipefail-sigpipe.test.sh`](../../scripts/tests/pipefail-sigpipe.test.sh)
(gauntlet shell-tests step). **No bypass.**

Companion to [`tests-determinism.md`](tests-determinism.md) and
[`test-value.md`](test-value.md). This governs the answer an assertion comes
back with.

Under `set -o pipefail`, a reader that exits early leaves the writer a failed
write, and that status becomes the pipeline's. A match that happened is reported
as no match — a flake where the assertion is `contains`, a false green where it
is `lacks`.

## The rules

### An assertion is not a pipeline

- Write the haystack as a here-string:

  ```bash
  grep -qF -- "$needle" <<<"$haystack"        # not: printf … | grep -qF …
  ```

- Where the haystack is an array, keep one element per line:

  ```bash
  grep -qxF "$needle" <<<"$(printf '%s\n' "${items[@]}")"
  ```

- Where a stage must run before the match, give it a variable rather than a
  pipe:

  ```bash
  links="$(grep -oE "$extract" <<<"$content" || true)"
  [ -n "$links" ] && grep -qvE "$allowed" <<<"$links"
  ```

### Any writer piped into a reader that can exit early is a race

- The readers are `grep -q`, `grep -m`, `grep -l`, `grep -L` and `head`.
- The writer is not part of the rule. Any command on the left of the pipe is
  subject to it.
- The tell is position: a pipeline whose status is read as a fact about the data
  — an `if`, an `&&`, an `||`, a bare statement.
- Redirection is not subject to it. `<<<`, `< file` and a command substitution
  hand the reader a source it can abandon without failing anybody.

## What the guard does

- **Demonstrates** the defect first, against a haystack sized to make the race
  certain, requiring the piped form to lose a needle that is present and the
  here-string form to find it.
- **Sweeps** every tracked shell script that enables `pipefail`, and counts what
  it read, so a sweep that reached nothing fails.
- Joins line continuations before matching.
- Removes command substitutions before matching, depth-aware, so what is judged
  is the pipeline's position rather than its text.
