# Assertion Determinism — An Assertion Survives Its Own Pipe

**Enforced by:**
[`scripts/tests/pipefail-sigpipe.test.sh`](../../scripts/tests/pipefail-sigpipe.test.sh)
(gauntlet shell-tests step). **No bypass.**

Companion to [`tests-determinism.md`](tests-determinism.md), which says a test
must always run, and to [`test-value.md`](test-value.md), which says what it must
run against. This says the answer it comes back with has to be the answer to the
question it asked.

## The defect

`printf '%s\n' "$haystack" | grep -qF -- "$needle"` reads as *does the haystack
contain the needle*. Under `set -o pipefail` it answers something else.

`grep -q` exits the moment it matches. When the haystack is larger than the pipe
will hold, `printf`'s remaining write fails with `EPIPE` and returns non-zero;
`pipefail` promotes that to the pipeline's status; and the caller reads a match
that *did* happen as no match at all:

| Written as | Needle present | Reported |
|---|---|---|
| `assert_contains` | yes | missing — a flake |
| `assert_lacks` | yes | absent — **a false green** |

The second is the shape [`ci-cd-determinism.md`](ci-cd-determinism.md) exists to
refuse: the check ran, the forbidden thing was there, and the gate went green.

It hides well. Nothing fires below the pipe's capacity, so every small fixture
passes forever. It needs a match *early* in a multi-line haystack with enough
left to write afterwards, and where the boundary falls depends on the machine —
so a workstation stays green while CI fails about one run in ten.

## The rule

### An assertion is not a pipeline

Write the haystack as a here-string. It appends the same trailing newline
`printf '%s\n'` did, so it matches identically, and the command's status is
`grep`'s alone:

```bash
grep -qF -- "$needle" <<<"$haystack"        # not: printf … | grep -qF …
```

Where the haystack is an array, keep the one-element-per-line shape the
line-oriented flags depend on — `grep -qx` means nothing against elements joined
onto a single line:

```bash
grep -qxF "$needle" <<<"$(printf '%s\n' "${items[@]}")"
```

Where a stage genuinely has to run before the match, give it a variable rather
than a pipe, so the early exit has nothing upstream to kill:

```bash
links="$(grep -oE "$extract" <<<"$content" || true)"
[ -n "$links" ] && grep -qvE "$allowed" <<<"$links"
```

### The generalisation

**Any writer piped into a reader that can exit early is a pipeline whose verdict
is a race**, and `pipefail` turns that race into a wrong answer rather than a
stray warning. `grep -q` is the common one; `head`, `grep -m` and `grep -l` are
the same shape. The tell is a pipeline in a position where its *status is read as
a fact about the data* — an `if`, an `&&`, a `test`.

Redirection is not subject to it: `<<<`, `< file` and a command substitution all
hand the reader a source it can abandon without failing anybody.

## What the guard does

[`pipefail-sigpipe.test.sh`](../../scripts/tests/pipefail-sigpipe.test.sh)
**demonstrates** the defect first — running both shapes against a haystack sized
to make the race certain, requiring the piped form to lose a needle that is
present and the here-string form to find it — so a guard that has stopped
reproducing anything fails rather than policing a non-problem. Then it **sweeps**
every tracked shell script that enables `pipefail`, and counts what it read, so a
sweep that reached nothing fails too.

The sweep joins line continuations before matching. A line-at-a-time sweep sees
none of the pipelines written across two lines, which is where the two most
consequential instances were — the commit guard's and the push guard's own verb
detection, which is the first thing either guard does.
