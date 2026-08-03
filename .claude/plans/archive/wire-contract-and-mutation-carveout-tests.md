# Wire-contract and mutation-carve-out test gaps

Source: `/tests-audit` run 2026-08-02, revised 2026-08-03 after empirical
verification. Scope: the Go→Rust control-message contract, and the mutation /
coverage carve-outs on the remote-session data path.

Every open question from the first draft is resolved below in **Decisions
(D1–D13)**. Nothing in this plan requires further input before implementation.

## The core defect — verified

Go's `ControlMessage` is one flat union struct in which every field but `Type`
is `omitempty`. Rust's `ControlMessage` is an internally-tagged enum whose
fields are required at decode unless marked `#[serde(default)]`. When a
Go-encoded server→agent message carries a zero-valued field, the key is dropped
from the wire map and the agent's decoder hard-errors.

Confirmed by encoding through the real Go codec and decoding through the real
`rmp_serde` path (throwaway probes, not committed):

| Go input | Wire bytes | Rust decode |
|---|---|---|
| `{Type: MsgRestartAgent}` | `81 a4 type ac RestartAgent` | `Err("missing field \`reason\`")` |
| `{Type: MsgAgentDeregistered}` | `81 a4 type b1 AgentDeregistered` | `Err("missing field \`reason\`")` |
| `{Type: MsgAgentUpdate, Version, URL}` | `83 …` (no `signature`) | `Err("missing field \`signature\`")` |
| `{Type: MsgSessionRequest}` | `81 a4 type ae SessionRequest` | `Err` (`token`, `relay_url`, `permissions` all absent) |
| `{Type: MsgRequestHardwareReport}` | `81 a4 type b5 …` | `Ok(RequestHardwareReport)` |
| `{Type: MsgRequestDeviceLogs}` | `81 a4 type b1 …` | `Ok` — all eight fields defaulted |

The agent routes any non-`Io` decode error to a catch-all that warns and
**breaks the control loop, forcing a full QUIC reconnect**
([`main.rs:958-960`](../../../agent/crates/mesh-agent/src/main.rs)).

### Complete blast radius

The server→agent write surface is thirteen variants — every `Send*` method on
`*agentapi.AgentConn`. Nine are already safe; four are not:

| Variant | Sent from | Rust fields required at decode | Status |
|---|---|---|---|
| `SessionRequest` | [`conn.go:220`](../../../server/internal/agentapi/conn.go) | `token`, `relay_url`, `permissions` | **fragile** |
| `AgentUpdate` | [`conn.go:230`](../../../server/internal/agentapi/conn.go) | `version`, `url`, `signature` (`sha256` defaulted) | **fragile** |
| `AgentDeregistered` | [`conn.go:241`](../../../server/internal/agentapi/conn.go), [`server.go:366`](../../../server/internal/agentapi/server.go) | `reason` | **fragile** |
| `RestartAgent` | [`conn.go:249`](../../../server/internal/agentapi/conn.go) | `reason` | **broken, reachable** |
| `RequestHardwareReport` | [`conn.go:257`](../../../server/internal/agentapi/conn.go) | — (unit variant) | safe |
| `RequestHealthWindow` | [`conn.go:267`](../../../server/internal/agentapi/conn.go) | none (all defaulted) | safe |
| `PushAlertRules` | [`conn.go:280`](../../../server/internal/agentapi/conn.go) | none | safe |
| `RequestLocalHistory` | [`conn.go:303`](../../../server/internal/agentapi/conn.go) | none | safe |
| `RequestDeviceLogs` | [`conn.go:317`](../../../server/internal/agentapi/conn.go) | none | safe |
| `MetricBackfillAck` | [`conn_backfill.go:91`](../../../server/internal/agentapi/conn_backfill.go) | none | safe |
| `GrantBackfill` | [`conn_backfill.go:124`](../../../server/internal/agentapi/conn_backfill.go) | none | safe |
| `DeferBackfill` | [`conn_backfill.go:130`](../../../server/internal/agentapi/conn_backfill.go) | none | safe |
| `SetMaintenanceMode` | [`conn_maintenance.go:16`](../../../server/internal/agentapi/conn_maintenance.go) | none | safe |

`SessionRequest` is the addition to the first draft, and it is the one that
matters most — it establishes every remote session. It is **not reachably broken
today**: `token` is always generated non-empty, `relayURL` is always a formatted
`scheme://host/ws/relay/token`, and `Permissions` is always `&perms`. It is
one refactor away from breaking, with no test that would notice.

Only `RestartAgent` is reachable from a client request. `RestartDeviceRequest.reason`
is `type: string` with no `minLength`
([`openapi.yaml:297`](../../../api/openapi.yaml)), OpenAPI constraints are not
enforced at runtime (tracked in [`techdebt.md`](../../techdebt.md)), and
`sanitizeText("")` returns `""`
([`validate.go:44`](../../../server/internal/api/validate.go)). So
`POST /api/v1/devices/{id}/restart` with `{"reason": ""}` returns **200**, the
device never restarts, and the agent drops its control stream.

### Why no gate catches it

- Forward goldens verify Rust-encode → Go-decode. Wrong direction, and they
  cannot see a Go **encoder** that omits a key the Rust **decoder** requires.
- The reverse golden for `RestartAgent` pins a non-empty reason
  ([`golden_reverse_test.go:143`](../../../server/internal/protocol/golden_reverse_test.go)).
  There is **no** reverse golden at all for `AgentUpdate`, `AgentDeregistered`,
  `RequestHardwareReport` or `RequestDeviceLogs` (16 reverse goldens exist; see
  [`testdata/golden/`](../../../testdata/golden/)).
- Handler tests assert the decoded `Reason` equals `"test restart"` /
  `"restart requested from web UI"`
  ([`handlers_restart_part2_test.go:65,87`](../../../server/internal/api/handlers_restart_part2_test.go)) —
  never the empty case.
- `restart.spec.ts` drives the UI, which always sends a non-empty reason.

## Decisions

**D1 — Where the wire fix lands: agent-side `#[serde(default)]`.**
Not a server-side `omitempty` removal. Two reasons the server side was
rejected: dropping `omitempty` on `Reason` in the flat union struct would emit
`reason: ""` on *every* message Go encodes, and the hand-written encoder
[`control_encode.go`](../../../server/internal/protocol/control_encode.go) is
contractually byte-identical to the reflection encoder
(`codec_wire_equivalence_test.go`), so a per-variant override would have to
break that invariant.

**D2 — `#[serde(default)]` only where the zero value is a legal value.**
This is the rule that decides each field, and it splits the four fragile
variants in two:

- `reason` on `RestartAgent` and `AgentDeregistered` is informational — the
  agent logs it and restarts / cleans up regardless. An empty reason is legal.
  → **default it.**
- `token`, `relay_url`, `permissions`, `version`, `url`, `signature` are
  load-bearing. An empty token, an unsigned update, or a session with no relay
  URL is not a message worth acting on. → **keep required at decode**, and make
  the server refuse to construct the frame (D3).

**D3 — Server-side send guards for the not-legal-empty fields.** `SendSessionRequest`
and `SendAgentUpdate` return an error instead of writing a frame the peer cannot
decode. This is what protects agents already in the field: they keep the strict
decoder until they update, so the frame must never leave the server.

**D4 — `{"reason": ""}` on restart returns 400, not 200.** Independent of the
wire fix. A 200 for a restart that provably did not happen is a defect on its
own. `minLength: 1` goes into the OpenAPI schema *and* a runtime check in the
handler, because OpenAPI constraints are not runtime-enforced here.

**D5 — Do not add `Default` derives to `SessionToken` or `Permissions`.** A
defaulted `SessionToken("")` would let the agent walk into a session with an
empty token, which is worse than a decode error. Follows from D2.

**D6 — Leave `AgentUpdate.sha256`'s existing `#[serde(default)]` in place.**
It is inconsistent with its siblings under D2, but `sha256` is verified against
the downloaded artifact by the agent's updater, so an absent value fails closed
at install time rather than at decode time. Not worth a wire change.

**D7 — A4's guard is a golden-completeness test, not a cross-language
reflection test.** With D1 chosen, a Go test cannot ask "is the Rust field
required?" — the answer now lives in Rust. Instead: reflect over
`*agentapi.AgentConn`'s exported `Send*` methods and assert each one appears in
the reverse-golden variant table. A new `Send*` method without a minimal-shape
golden fails the test, and the Rust verifier then proves that shape decodes.

**D8 — Minimal-shape goldens are named `…_min`, not `…_empty_<field>`.** One
extra golden per fragile variant pinning the smallest map the server can emit,
rather than a per-field matrix. The variant, not the field, is the unit of the
contract.

**D9 — Phase A lands as one commit.** The serde defaults, the send guards, the
400, the goldens and the completeness test are one contract change; splitting
them leaves a window where the goldens pin a shape the agent rejects.

**D10 — Rust *mutation* carve-outs need no work.**
[`agent/.cargo/mutants.toml`](../../../agent/.cargo/mutants.toml) already carries a
per-entry justification for every `exclude_globs` and `exclude_re` entry,
including an explicit cross-reference to the web-side carve-outs. The first
draft's premise that the Rust side was unjustified was wrong.

**D11 — `session/mod.rs` is a *coverage* carve-out, not a mutation one.** It is
not in `mutants.toml`; it is in `ci.yml`'s `--ignore-filename-regex`
([`ci.yml:89`](../../../.github/workflows/ci.yml)). cargo-mutants already mutates
it. Two different gates, and the first draft conflated them.

**D12 — Cover `session/mod.rs` and remove it from the coverage ignore regex.**
Feasible without a live network: `run()`/`receive_loop()` take a real
`MaybeTlsStream<TcpStream>` WebSocket, so a test drives them through an
in-process `TcpListener` + `accept_async` relay stub with `NullCapture` /
`NullInput`. `webrtc.rs`, `terminal.rs`, `relay.rs` and `main.rs` stay ignored.
Gate risk to respect: `cargo llvm-cov` runs `--fail-under-lines 80`
**workspace-wide**, and the same `lcov.info` feeds SonarCloud, so the file must
be genuinely covered *before* the regex entry is deleted — in that order, one
commit.

**D13 — The mutation gate has no baseline "to be taken".** It compares against
the previous successful run fetched from VictoriaMetrics
([`mutation-baseline-fetch.sh`](../../../scripts/mutation-baseline-fetch.sh)) and
also enforces an absolute floor. So the first draft's "Phase B has to land
before the next baseline is taken" is not a real constraint. The real one:
**B1 and B2 must land in the same commit**, because un-excluding files without
killing their survivors drops the score and trips the regression gate on the
next nightly.

## Phase A — close the wire contract (~1–2 days)

**A1. Agent-side defaults + decode tests.** In
[`control.rs`](../../../agent/crates/mesh-protocol/src/control.rs), add
`#[serde(default)]` to `RestartAgent.reason` and `AgentDeregistered.reason`
(D2). Add a Rust unit test per variant that decodes the minimal `{"type": X}`
map and asserts every field sits at its zero value. Add the negative half: a
minimal `SessionRequest` / `AgentUpdate` map still fails to decode, and the
error names the missing field.

**A2. Server-side send guards.** In
[`conn.go`](../../../server/internal/agentapi/conn.go), `SendSessionRequest`
rejects an empty `token` or `relayURL`; `SendAgentUpdate` rejects an empty
`version`, `url` or `signature`. Return a typed error in the shape of the
existing `ErrCapabilityNotAdvertised` so handlers can classify it. Table-driven
tests for each field, positive and negative.
[`handlers_updates.go:97`](../../../server/internal/api/handlers_updates.go) must
surface the guard as a push failure rather than a silent skip — an unsigned
update push is a 4xx, not a no-op.

**A3. Restart validation → 400.** Add `minLength: 1` and a `maxLength` to
`RestartDeviceRequest.reason` in [`openapi.yaml`](../../../api/openapi.yaml),
regenerate both clients (`oapi-codegen`, `npm run generate:api`), and add the
runtime check in the restart handler. Tests: `{"reason": ""}` → 400, absent
`reason` → 400, whitespace-only → 400, valid → 200 and a decoded `RestartAgent`
reaches the agent. Mirror the negative cases into
[`handlers_restart_part2_test.go`](../../../server/internal/api/handlers_restart_part2_test.go).

**A4. End-to-end regression test.** In
[`server/tests/integration/`](../../../server/tests/integration/), using
`agentTestEnv.connectAgent`
([`agentapi_test.go:205`](../../../server/tests/integration/agentapi_test.go)),
drive `POST /devices/{id}/restart` against a connected agent and assert on the
frame the agent actually receives: a valid reason decodes to a complete
`RestartAgent`, and `{"reason": ""}` never puts a frame on the stream at all.

**A5. Reverse goldens.** Add to `writeReverseControlFrame` in
[`golden_reverse_test.go`](../../../server/internal/protocol/golden_reverse_test.go)
and to the verifier
[`reverse_golden_test.rs`](../../../agent/crates/mesh-protocol/tests/reverse_golden_test.rs):

- `go_control_agent_update.bin` — no reverse golden exists today
- `go_control_agent_deregistered.bin` — no reverse golden exists today
- `go_control_agent_deregistered_min.bin` — empty reason, decodes after A1
- `go_control_restart_agent_min.bin` — empty reason, decodes after A1
- `go_control_request_hardware_report.bin` — pins the unit-variant no-field shape
- `go_control_request_device_logs.bin` — pins the all-defaults shape

No `_min` golden for `SessionRequest` or `AgentUpdate`: under D2/D3 that frame
is unconstructible, and A2's guard tests are what pin it.

**A6. Golden-completeness guard.** Per D7: a Go test that reflects over
`*agentapi.AgentConn`, collects exported `Send*` methods, and asserts each is
present in an explicit method→golden-variant table. Adding a server→agent write
without a reverse golden fails.

**Done when:** `{"reason": ""}` restart returns 400; a valid restart round-trips
to a decoded `RestartAgent` over a real QUIC stream; the send guards reject
every not-legal-empty field with a test each; the six new goldens are verified
by the Rust harness; the golden drift gate stays green; A6 fails when a
`Send*` method is added without a golden.

## Phase B — mutation and coverage carve-outs (~2–3 days)

[`web/stryker.config.json`](../../../web/stryker.config.json) excludes six product
source files from mutation with **no justification comment**, contrary to the
carve-out doctrine that
[`agent/.cargo/mutants.toml`](../../../agent/.cargo/mutants.toml) follows properly:

| Excluded file | Unit test exists? |
|---|---|
| `src/features/terminal/use-terminal.ts` | No |
| `src/features/remote-desktop/use-remote-desktop.ts` | No |
| `src/features/remote-desktop/input-handler.ts` | **Yes** (`input-handler.test.ts`) |
| `src/features/session/state/connection-store.ts` | **Yes** (`connection-store.test.ts`) |
| `src/lib/transport/webrtc-transport.ts` | **Yes** (`webrtc-transport.test.ts`) |
| `src/features/admin/NotificationCenter.tsx` | **Yes** |

Four of the six *have* tests — the exclusion is not "no test harness", it is
"test quality never measured" on the session transport and connection state
machine. Combined with `session/mod.rs` being invisible to coverage (D11), the
remote-session data path is the one subsystem excluded from a quantitative gate
on both ends.

**B1.** Measure before deleting anything: run `make mutate-web` scoped to the
four tested files and record the survivor list. This is what tells you whether
un-excluding them clears the absolute floor once B2 lands.
**B2.** Write mutant-killing tests for the survivors — assert the changed return
value or state transition, not a side effect both variants share. Then remove
those four exclusions. **Same commit as B1's removals** (D13).
**B3.** Keep the two untested exclusions (`use-terminal.ts`,
`use-remote-desktop.ts`) **with a one-line justification each**, in the style
`mutants.toml` already uses. Every remaining entry in the `mutate` array gets a
justification, including the pre-existing `main.tsx` / `router.tsx` /
`icons.tsx` / `*.d.ts` entries.
**B4.** Per D12: write in-process-relay-stub tests for
[`session/mod.rs`](../../../agent/crates/mesh-agent-core/src/session/mod.rs)
(`new`, `with_ice_servers`, `run`'s permission-gated task spawning,
`receive_loop`'s ping/close/empty/decode-error branches, `cleanup`), confirm
`cargo llvm-cov` still clears `--fail-under-lines 80` with the file measured,
*then* remove `/session/mod\.rs` from the ignore regex and add per-entry
justification comments for the entries that remain.

**Done when:** every entry in `stryker.config.json`'s `mutate` array and in
`ci.yml`'s `--ignore-filename-regex` carries a justification; no excluded file
has passing unit tests that are simply unmeasured; `session/mod.rs` is measured
and the workspace still clears its coverage floor.

## Constraint — mutation stays nightly

`mutation.yml` runs on `cron: 0 3 * * *` and its `gate` job is deliberately
**not** on `merge-to-main.needs[]`. Mutation testing is far too slow to sit on
the merge path, and it stays a nightly run permanently. Do not propose moving
it to PR CI. The nightly `gate` job remains the enforcement point — it fails the
workflow red on a score regression against the previous successful run, which is
what keeps Phase B's newly-measured files from silently rotting.

## Confirmed covered — do not re-audit

- **E2E feature matrix**: 19 specs / 85 tests. Session+terminal, file manager,
  device logs, chat, restart, hardware, capability tabs, push, a11y
  (`@axe-core`), visual regression (`toHaveScreenshot`), authorization model —
  all present. Zero `waitForTimeout`.
- **Goldens**: bidirectional, 46 forward + 16 reverse, `git diff --exit-code`
  drift gate, `golden` job on `merge-to-main.needs[]`, per-golden
  `protocol_version` sidecars, edge corpus (utf8, empty collection,
  forward-compat unknown key, large payload, little-endian length header).
- **Encoder equivalence**: the hand-written `EncodeMsgpack` is diffed field by
  field against the reflection encoder, with an allocation budget and a
  reflection-drift test on `controlFieldCount`.
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
- **Flakiness**: 3 real `time.Sleep` sites in tests. Not worth a finding.
