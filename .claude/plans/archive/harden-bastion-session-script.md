# Harden `deploy/scripts/bastion-session.sh` for debuggability

## Context

Three failure modes in `bastion-session.sh` wasted operator debug time over the last 24 h:

1. **`--session-ttl-in-seconds` vs `--session-ttl`** — I used the API field name as a CLI flag, OCI CLI rejected it with `No such option`, and the script wrapped the failure in a generic `"Check IAM"` hint that misdirected the investigation.
2. **`2>/dev/null` on every `oci ...` call** swallowed real OCI errors — IAM problems, validation errors, quota errors, flag-name typos — leaving only the generic hint.
3. **No persistent log file**, no debug knob, no read-only sanity-check command. To diagnose anything the operator had to manually re-issue OCI CLI commands.

A secondary observation surfaced during smoke-tests: my own interrupted verification (Ctrl-C of `make tunnel` mid-create) left an **orphan ACTIVE session** on the bastion. The previous script would have ignored it and consumed a second quota slot on a duplicate session. That's a recurring class of mistake worth designing out.

The intended outcome is a single-file rewrite that makes the next failure self-diagnosing: the operator runs `bastion-session.sh diagnose`, sees state, and the persistent log carries the last failure's stderr verbatim.

## Approach

Rewrite [`deploy/scripts/bastion-session.sh`](../../deploy/scripts/bastion-session.sh) in-place (no new files, no Makefile changes, no docs changes). The rewrite is mechanical — preserve the existing `tunnel` / `ssh` subcommands and cache file format, layer instrumentation around them.

Decisions (confirmed with user):

- **Orphan-session reuse is default-on.** Before creating a new session, list ACTIVE sessions on the bastion and reuse any that match (target instance, target user) with > headroom TTL remaining. Always announces via a log line.
- **No `make diagnose` target.** Operators invoke `./deploy/scripts/bastion-session.sh diagnose` directly.
- **Single focused commit.** No Makefile or docs changes in this PR.

### What changes

| Surface | Behavior |
|---|---|
| `set -Eeuo pipefail` + `trap on_err ERR` | Failures emit `line N (exit C): <failing command>` and point at the persistent log. Functions inherit the trap via `-E`. |
| `OPENGATE_BASTION_DEBUG=1` env | Enables `set -x` AND passes `--debug` to every OCI CLI call. Everything else stays the same. |
| `~/.cache/opengate/bastion-session.log` | Rolling log (5 MB cap → moves to `.1`). Every run appends timestamped INFO/WARN/ERROR/DEBUG entries. OCI stderr is captured verbatim on failure. |
| `oci_cmd <args...>` wrapper | Single source of truth for invocation. Captures stderr, surfaces on failure (no more `2>/dev/null` swallowing). |
| `--session-ttl 10800` | Fixed CLI-flag name typo from earlier patch; reads granted TTL from `session get` response into the cache (defensive against future bastion-side TTL clamps). |
| `find_reusable_session` | jq-filtered scan of `bastion session list --session-lifecycle-state ACTIVE`. Used by `create_session` as a first-chance check. |
| `diagnose` subcommand | Prints: prerequisites (oci/jq/terraform/ssh versions), resolved inputs (OCIDs + IP + user + key path), bastion state (`bastion get`), active-session list, Cloud Agent Bastion plugin status, local cache state, log file size. Pure read-only. |
| `purge` subcommand | Deletes the cache file. Next run creates fresh. |
| Prerequisites block | `${var%%$'\n'*}` instead of `cmd \| head -1` — sidesteps a spurious `(not found)` line that the pipefail + SIGPIPE interaction was producing for `terraform version`. |

### Critical files

- [`deploy/scripts/bastion-session.sh`](../../deploy/scripts/bastion-session.sh) — full rewrite (~250 lines net change, single file).

Reuses existing helpers in the same file (`now_epoch`, the existing cache shape, the OCI-emitted ProxyCommand patching via `sed`). No new external dependencies (`jq`, `oci`, `terraform` were already required).

### Verification

Already executed successfully against the live OCI bastion before this plan was written:

1. **`shellcheck`** clean (one JMESPath false-positive suppressed inline with justification).
2. **`bastion-session.sh diagnose`** — prints all 6 sections, surfaces the orphan session OCID, shows bastion `ACTIVE`, plugin `RUNNING`, prerequisites versions, empty cache.
3. **`bastion-session.sh tunnel`** (with empty cache + an orphan ACTIVE session on the bastion) — log shows `"Reusing pre-existing ACTIVE session ... (orphan from a prior interrupted run)"`, cache written with `expires_in_min ≈ 180` (the full 3 h TTL OCI granted).
4. **`./scripts/precommit-gauntlet.sh`** — ALL CHECKS PASSED in 382s (lints, codegen, tests, coverage, audits, benchmarks, e2e, sonar-quick).

End-to-end confidence: the failure mode that started this session (`oci ... failed. Check IAM`) is no longer reachable for the documented bug — the CLI's actual error (`No such option: --session-ttl-in-seconds`) would surface verbatim AND be appended to the log. For any future failure, `tail ~/.cache/opengate/bastion-session.log` is the first move.
