# Wire-contract and mutation-carve-out test gaps

Source: `/tests-audit` run 2026-08-02. Scope: Go integration tests, Playwright
E2E, cross-language goldens, mutation carve-outs.

The suite is in good shape — the E2E feature matrix is complete, goldens are
bidirectional and drift-gated, `-race` is on both Go jobs, and Postgres-native
behavior has targeted tests. Everything below is a gap that survives the current
CI pipeline.

## The core defect

Go's `ControlMessage` tags **every** field `omitempty`. Rust's `ControlMessage`
is an internally-tagged enum whose fields are mostly **required at decode**
(no `#[serde(default)]`, not `Option`). When a Go-encoded server→agent message
carries a zero-valued field, the key is dropped from the wire map and the
agent's decoder hard-errors.

Proven end-to-end:

| Go input | Wire bytes | Rust decode |
|---|---|---|
| `{Type: MsgRestartAgent}` | `81 a4 type ac RestartAgent` | `Err("missing field \`reason\`")` |
| `{Type: MsgAgentDeregistered}` | `81 a4 type b1 AgentDeregistered` | `Err("missing field \`reason\`")` |
| `{Type: MsgAgentUpdate, Version, URL}` | `83 …` (no `signature`) | `Err("missing field \`signature\`")` |

The agent's control loop routes any non-`Io` decode error to a catch-all that
warns and **breaks the loop, forcing a full QUIC reconnect**
([`main.rs:958-960`](../../agent/crates/mesh-agent/src/main.rs)).

`RestartAgent` is reachable from an unauthenticated-by-shape client request:
`RestartDeviceRequest.reason` is `type: string` with no `minLength`
([`openapi.yaml:297`](../../api/openapi.yaml)), OpenAPI constraints are not
enforced at runtime (tracked in [`techdebt.md`](../techdebt.md)), and
`sanitizeText("")` returns `""`
([`validate.go:44`](../../server/internal/api/validate.go)). So
`POST /api/v1/devices/{id}/restart` with `{"reason": ""}` returns **200**, the
device never restarts, and the agent drops its control stream.

Why no gate catches it:

- Forward goldens verify Rust-encode → Go-decode. For a server→agent message
  that is the wrong direction, and it cannot see a Go **encoder** that omits a
  key the Rust **decoder** requires.
- The reverse golden for `RestartAgent` pins a non-empty reason
  ([`golden_reverse_test.go:143`](../../server/internal/protocol/golden_reverse_test.go)).
- Handler tests assert the decoded `Reason` equals `"test restart"` /
  `"restart requested from web UI"`
  ([`handlers_restart_part2_test.go:65,87`](../../server/internal/api/handlers_restart_part2_test.go)) —
  never the empty case.
- `restart.spec.ts` drives the UI, which always sends a non-empty reason.

## Phase A — close the wire contract (~1–2 days)

**A1. Regression test for the reachable path.**
In `server/tests/integration/`, drive `POST /devices/{id}/restart` with
`{"reason": ""}` against a connected fake agent and assert the frame the agent
receives decodes into a complete `RestartAgent`. Add the mirrored negative case
to `handlers_restart_part2_test.go`.

**A2. Zero-value reverse goldens.**
Add to the generator in
[`golden_reverse_test.go`](../../server/internal/protocol/golden_reverse_test.go)
(the `writeReverseControlFrame` block starting line 112) and to the verifier
`agent/crates/mesh-protocol/tests/reverse_golden_test.rs`:

- `go_control_restart_agent_empty_reason.bin`
- `go_control_agent_deregistered.bin` **and** `…_empty_reason.bin`
- `go_control_agent_update.bin` **and** `…_empty_signature.bin`
- `go_control_request_hardware_report.bin` (unit variant — pins the no-field shape)
- `go_control_request_device_logs_all_defaults.bin`

`AgentUpdate`, `AgentDeregistered`, `RequestHardwareReport` and
`RequestDeviceLogs` are Go-encoded at
[`conn.go:232,243,262,324`](../../server/internal/agentapi/conn.go) and
[`server.go:366`](../../server/internal/agentapi/server.go) and have **no**
reverse golden at all today.

**A3. Pick the fix, then encode it in the goldens.** Two coherent options —
this needs a decision before A2 is written, because the goldens pin whichever
is chosen:

- *Server-side*: drop `omitempty` from fields that are required on the Rust
  side, so the zero value is transmitted.
- *Agent-side*: add `#[serde(default)]` to the required-at-decode fields of
  Go-encoded server→agent variants.

Recommendation: **server-side**. It keeps the agent strict about genuinely
malformed frames, and the wire cost is a handful of empty strings on
low-frequency control messages.

**A4. Structural guard.** A Go test that reflects over `ControlMessage`'s
`msgpack` tags and fails when a field used by a Go-encoded server→agent message
carries `omitempty` while the corresponding Rust field is required. Without
this, the next added field reopens the hole.

**Done when:** `{"reason": ""}` restart round-trips to a decoded
`RestartAgent`; the new reverse goldens exist and are verified by the Rust
harness; the golden drift gate stays green; A4 fails if `omitempty` is
reintroduced on a required field.

## Phase B — mutation carve-outs (~2–3 days)

[`web/stryker.config.json`](../../web/stryker.config.json) excludes six product
source files from mutation with **no justification comment**, contrary to the
carve-out doctrine:

| Excluded file | Unit test exists? |
|---|---|
| `src/features/terminal/use-terminal.ts` | No |
| `src/features/remote-desktop/use-remote-desktop.ts` | No |
| `src/features/remote-desktop/input-handler.ts` | **Yes** |
| `src/features/session/state/connection-store.ts` | **Yes** |
| `src/lib/transport/webrtc-transport.ts` | **Yes** |
| `src/features/admin/NotificationCenter.tsx` | **Yes** |

Four of the six *have* tests — the exclusion is not "no test harness", it is
"test quality never measured" on the session transport and connection state
machine. The same subsystem is excluded on the Rust side: `rust-test`'s
`--ignore-filename-regex` drops `/webrtc.rs`, `/terminal.rs`, `/session/mod.rs`,
`/session/relay.rs` from coverage
([`ci.yml:89`](../../.github/workflows/ci.yml)), and
`agent/crates/mesh-agent-core/src/session/mod.rs` has **zero** inline tests.

So the remote-session data path is the one subsystem excluded from the
quantitative gate on *both* ends.

**B1.** Remove the four exclusions whose files already have tests; run
`make mutate-web` scoped to them and record survivors.
**B2.** Write mutant-killing tests for the survivors (assert the changed return
value / state transition, not a side effect both variants share).
**B3.** For anything genuinely unmutateable, keep the exclusion **with a
one-line justification** next to the entry.
**B4.** Same treatment for `session/mod.rs`: either cover it and drop it from
`--ignore-filename-regex`, or record why it cannot be.

**Done when:** every remaining exclusion in `stryker.config.json` and in the
Rust `--ignore-filename-regex` carries a justification, and no excluded file
has passing unit tests that are simply unmeasured.

## Constraint — mutation stays nightly

`mutation.yml` runs on `cron: 0 3 * * *` and its `gate` job is deliberately
**not** on `merge-to-main.needs[]`. Mutation testing is far too slow to sit on
the merge path, and it stays a nightly run permanently. Do not propose moving
it to PR CI.

The nightly `gate` job remains the enforcement point: it fails the workflow red
on a score regression against the stored baseline, which is what keeps Phase B's
newly-measured files from silently rotting. Phase B therefore has to land
before the next baseline is taken, so the baseline reflects real coverage rather
than unmeasured carve-outs.

## Confirmed covered — do not re-audit

- **E2E feature matrix**: 19 specs / 85 tests. Session+terminal, file manager,
  device logs, chat, restart, hardware, capability tabs, push, a11y
  (`@axe-core`), visual regression (`toHaveScreenshot`), authorization model —
  all present. Zero `waitForTimeout`.
- **Goldens**: bidirectional, 46 forward + 16 reverse, `git diff --exit-code`
  drift gate, `golden` job on `merge-to-main.needs[]`, per-golden
  `protocol_version` sidecars, edge corpus (utf8, empty collection,
  forward-compat unknown key, large payload, little-endian length header).
- **Negative paths**: 43 `StatusNotFound`, 36 `StatusForbidden`, 31
  `StatusUnauthorized`, 29 `StatusBadRequest`, 9 `StatusConflict`, 4
  `StatusTooManyRequests` across the Go suite. The apparent "30 of 65 api test
  files have no negative-path assertion" is an artifact of the `_partN_test.go`
  split — e.g. `handlers_restart_test.go` is a fixture file and the tests live
  in `handlers_restart_part2_test.go`.
- **Postgres-native**: `postgres_native_test.go` covers TIMESTAMPTZ offset
  normalization, JSONB `network_interfaces` round-trip, UUID case/malformed
  boundaries, concurrent upsert, prepared-statement cache reuse.
- **Race/concurrency**: `-race` on both `go-unit` and `go-integration`; 11
  integration files use `errgroup`/`WaitGroup`; 431 `t.Parallel()` sites.
- **Flakiness**: 3 real `time.Sleep` sites in tests (2 further hits are comments
  documenting their removal). Not worth a finding.
