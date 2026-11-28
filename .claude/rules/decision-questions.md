# Decision Questions — A Question Shows Its Research

**Enforced by:**
[`.claude/hooks/pretooluse-question-research-guard.sh`](../hooks/pretooluse-question-research-guard.sh)
(the two searches) and a second model, a `prompt` hook on `AskUserQuestion` in
[`settings.json`](../settings.json) (what the question carries). **No bypass.** Why:
[ADR-080](../../docs/adr/ADR-080-documentation.md).

Companion to [`plans-and-adrs.md`](plans-and-adrs.md), which holds a plan to nothing open before
it is set active. This governs every question put to Ivan.

## The rules

### Every question goes in the modal

- A question or a decision for Ivan is asked through the `AskUserQuestion` modal, never as a list
  at the end of a reply. The reply around it only reports.
- Each question stands on its own: what happened, why it matters, and the options, so a reader who
  sees only the modal can answer it.
- Up to four questions a call, the recommended option first.

### Research comes before the options

- Since Ivan's last message, two searches come first: a web search on how others do it, and a
  search of git and GitHub Actions history for an earlier attempt here.
- The guard reads the session log and refuses a modal missing either one. A tool result, a modal
  answer included, is not a message Ivan typed, so it does not reset the count.
- Research covers every decision: architecture, CI, tests, tooling.

### What each option carries

- **Plain words.** The thing itself, never a term of art standing in for an explanation: "how many
  separate measurements the server keeps per machine", not "cardinality". A product or protocol
  name with no plain equivalent is used once and defined in the same sentence.
- **Its good and its bad**, each in its own words.
- **How a named product builds that same option**, with a source — NinjaOne, Netdata,
  MeshCentral, Datto, Intune, a standard. A table in the reply is not enough; the option itself
  says it.
- **Its effect on the code base's speed** — the product's or the tooling's, measured where it can
  be (time the hook, the parse, the call, the gauntlet), or said to be unmeasured.
- **The upkeep of the code it adds**: lines to keep, tests, what breaks it on an update. What the
  research itself cost is none of these.
- **A concrete scenario** of what a technician sees under it: "Patch Tuesday breaks 40 machines at
  Contoso Dallas; under this option that is one investigation, under that one 312".

### Every outside product and every test says what it is for

- An outside product, service or tool named says in one plain sentence what it is and why it bears
  on this choice. A threat named says how it would happen in OpenGate.
- A test or check named says what it catches: "it checks X, so that Y cannot happen unnoticed".

### A test is argued against before it is proposed

- A test is proposed only after arguing it is not needed. It stays only when the failure it catches
  would otherwise pass unnoticed.
- A failure that shows itself at once — everything refused, an empty answer, a commit that takes
  its whole time limit — needs no test.

### Answers are checked against earlier answers

- Each answer is read against the answers before it, and a conflict between two is asked about at
  once.

## How the checks read

- The guard finds Ivan's last typed message in the log at the hook input's `transcript_path`, and
  after it a `WebSearch`, `WebFetch` or `web-fetch` agent call, and a `git log`, `git show`,
  `git blame`, `gh run` or `gh api` call on `/actions`, or a GitHub commit search.
- A log the guard cannot read refuses the modal, so a log format a Claude Code update changes shows
  as a refusal on the first modal.
- The second model reads the modal before it opens and refuses one breaking the rules above; its
  reason comes back to the session, which mends the modal and asks again. It answers a refusal when
  it receives no modal text.
