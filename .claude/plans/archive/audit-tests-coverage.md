# Audit Plan — Test Coverage Sweep

**Skill:** `/tests-audit` (produces a plan for review — never implements fixes).
**Branch:** `dev`. **Owner:** engineer (Go + web E2E).
**Date:** 2026-06-27. **Status:** Ready for review.

## Scope & method

Read [`.github/workflows/ci.yml`](../../../.github/workflows/ci.yml) first, then
inventoried Go integration tests, Playwright E2E, and cross-language goldens.
Only gaps the **existing CI will not catch** are reported; everything CI already
gates is recorded as clean. The skill's own example "missing E2E" matrix is
**out of date** — `session-terminal`, `file-manager`, `device-logs`,
`capability-tabs` specs now exist.

## Confirmed clean / already gated (evidence)

- **Coverage gates:** Rust `--fail-under-lines 80`
  ([`ci.yml:115`](../../../.github/workflows/ci.yml#L115)); Go threshold over
  prod-excluded paths ([`ci.yml:233`](../../../.github/workflows/ci.yml#L233)); Web
  threshold ([`ci.yml:404`](../../../.github/workflows/ci.yml#L404)); plus the
  SonarCloud new-code gate.
- **`-race`** on both `go-unit` and `go-integration`
  ([`ci.yml:208,268`](../../../.github/workflows/ci.yml#L208)).
- **Goldens bidirectional:** forward (Go decodes Rust-encoded) **and** reverse
  (Go encodes) + sidecars in the `golden` job
  ([`ci.yml:304-307`](../../../.github/workflows/ci.yml#L304)); Phase A added 10
  cross-boundary goldens (session/file/chat/agent_update) + 5 edge-case goldens
  (empty / UTF-8 / >64 KiB / forward-compat / LE-length).
- **Integration breadth** (`server/tests/integration/`): relay faults,
  control-stream faults, postgres-native invariants, signaling/relay, reconnect,
  agentapi, AMT, admin, security groups, session, WS middleware, auth edges.
- **Concurrency on the shared agent stream:** `AgentConn.sendControl` serializes
  via `writeMu sync.Mutex`
  ([`conn.go:46,95`](../../../server/internal/agentapi/conn.go#L46)); covered
  indirectly under `-race`.
- **Mutation / property / fuzz:** wired across all three languages (techdebt
  "test-technique gaps" is RESOLVED); one known equivalent mutant in
  `handler.rs` is accepted with a pay-down trigger.

## Findings (gaps CI will not catch)

| # | Sev | Gap | Has lower-level coverage? | Location |
|---|-----|-----|---------------------------|----------|
| 1 | MEDIUM | **Web Push** subscribe → VAPID → service-worker → delivery has no E2E (keywords `notification`/`subscribe` absent from `web/e2e/`). | Yes — [`push_handlers_test.go`](../../../server/internal/api/push_handlers_test.go) | `web/e2e/` (no `push.spec.ts`) |
| 2 | MEDIUM | **Agent Restart** button flow (confirm → restart → device-state reflect) has no E2E (`restart` absent from `web/e2e/`). | Yes — [`handlers_restart_test.go`](../../../server/internal/api/handlers_restart_test.go) | `web/e2e/` (no restart spec) |
| 3 | MEDIUM | **Chat / messaging** has no end-to-end send→echo E2E (`chat` appears only in `capability-tabs.spec.ts` as tab-presence). | Partial — chat goldens + agent handlers | `web/e2e/` (no `chat.spec.ts`) |
| 4 | LOW | **Hardware inventory** E2E is incidental (keyword appears in 4 specs as mentions); verify a spec actually asserts inventory fetch+render, not just tab presence. | Yes — device hardware handler tests | `web/e2e/capability-tabs.spec.ts` |
| 5 | LOW | No **dedicated concurrent `sendControl`** test pinning `writeMu`'s intent (mutex exists + `-race` on, so risk is low). | Indirect (race detector) | `server/tests/integration/control_stream_faults_test.go` |

## Recommended plan (by effort)

### Phase A — close the MEDIUM E2E gaps (~2–3 days)

1. **F2 Restart E2E** (cheapest, highest-value): `web/e2e/restart.spec.ts` — log
   in, open a device with the restart capability, click Restart, confirm, assert
   the optimistic UI + the device transitions (mock/stub the agent side as the
   other specs do). *(Done-when: spec passes in the `e2e` job.)*
2. **F3 Chat E2E**: `web/e2e/chat.spec.ts` — open the chat tab, send a message,
   assert the echoed message renders (reuse the wire-level harness pattern from
   `file-manager.spec.ts` that encodes real msgpack control frames).
3. **F1 Web Push E2E**: scope realistically — browser push delivery is hard to
   drive in Playwright. Cover the **subscribe** half deterministically (grant
   notification permission, assert the subscription POST to the push API +
   VAPID-key fetch). Treat actual OS-level delivery as out of automated scope and
   document why. *(Done-when: subscribe flow asserted; delivery explicitly
   deferred.)*

### Phase B — depth + hardening (~1 day)

4. **F4:** add an explicit assertion in `capability-tabs.spec.ts` (or a new spec)
   that hardware inventory values render after fetch, not just that the tab shows.
5. **F5:** add a focused integration test issuing N concurrent `sendControl`
   calls to one `AgentConn` under `-race`, asserting all frames are well-formed
   (pins the `writeMu` contract).

## File inventory

**Create:** `web/e2e/restart.spec.ts`, `web/e2e/chat.spec.ts`,
`web/e2e/push.spec.ts`; a concurrent-send case in
`server/tests/integration/control_stream_faults_test.go`.
**Modify:** `web/e2e/capability-tabs.spec.ts` (hardware assertion).

## Acceptance criteria

1. Restart, chat, and push-subscribe E2E specs pass in the `e2e` job; no
   `waitForTimeout` hard sleeps; each test has ≥1 `expect`.
2. Concurrent `sendControl` test passes under `-race`.
3. No new flake: specs wait on health/conditions, not fixed delays.

## Reviewer checklist

- [ ] Each new E2E asserts behavior (not navigate-only); no `.only`/`.skip`.
- [ ] Push E2E covers subscribe and explicitly documents delivery as out-of-scope.
- [ ] Concurrent `sendControl` test fails if `writeMu` is removed.
- [ ] No duplication of a check CI already enforces (coverage, goldens, race).
