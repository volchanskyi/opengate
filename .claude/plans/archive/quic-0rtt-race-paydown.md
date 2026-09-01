# Micro-Plan: Delete the 0-RTT test and guard the setting it stood for

**Status:** landed 2026-09-01. **Branch:** `dev`.
**Owner:** server (Go).
**Register entry:** removed from [`techdebt.md`](../../techdebt.md) on
2026-08-31, ahead of implementation.
**Decision record:** [ADR-037](../../../docs/adr/ADR-037-client-first-fast-path-reconnect.md).
**Verified against the working tree 2026-08-31.**

Two changes, plus the paperwork:

1. **Delete `TestQUIC0RTT_ClientCertBehaviour`.** It is the only thing in the
   repo that switches early data on, and it is what makes the gate fail at
   random. Nothing replaces it.
2. **Add `TestProductionListenerRefusesEarlyData`** in `internal/agentapi` — the
   real listener keeps early data off, which is what was actually decided and
   what nothing checks today.

The tests **stay where they are**. No package move, no new helper package, no
config changes, and the two links from ADR-037 stay valid because the file
survives.

---

## 1. Why the deleted test can go

It is named for early data but never checks it. Walking every way it can fail:
the certificate manager won't build, its own test server won't start, the first
connection fails, no session ticket arrives in 5s, the second dial errors,
opening a stream or the byte round-trip fails, the handshake doesn't finish in
5s, or the server ends up holding no client certificate.

That is the whole list. In plain terms it checks **that you can connect twice
with our certificates and that the server sees the client's certificate.**

| # | Fact | Evidence |
|---|---|---|
| A1 | `Used0RTT` is written to the log and never compared to anything. | file read |
| A2 | Running its exact body with early data switched **off** at the server passes, 3/3, printing `Used0RTT=false` on both sides. **The test named for early data passes with early data disabled.** | measured 2026-08-31 |
| A3 | Its one real assertion — the server holds a client certificate — is made more strictly by `TestQUICSessionResumption_PreservesMTLSIdentity` three functions above it, which also checks both sides report a resumed session and that the certificate carries a common name. | file read |
| A4 | Nothing in the repo uses early data: no `DialAddrEarly` and no `Allow0RTT` outside this one test, in Go or Rust. ADR-037 deferred it. | repo-wide grep |
| A5 | ⟹ It is a weaker copy of the resumption test, and the one thing it does differently — switching early data on — is the cause of the random failures. | A1–A4 |

The two tests that stay **do** earn their place: the cold-handshake test proves
the certificate settings enforce mutual TLS, and the resumption test is named by
ADR-037 as the always-run guard for a decision that **was** adopted.

## 2. Why the guard is the thing that was missing

ADR-037 decided to keep early data off on the server. Today `Allow0RTT` appears
exactly once in `internal/` — in the test being deleted, setting it to **true**.
The production `quic.Config` at
[`server.go:201`](../../../server/internal/agentapi/server.go#L201) simply omits
it, and **nothing anywhere asserts that it stays omitted**. One added line would
turn early data on with no test going red.

[`newAcceptEnv`](../../../server/internal/agentapi/server_accept_test.go) already
starts the real `ListenAndServe` on loopback, so the guard needs **no change to
production code**. Measured against it, 3/3: a session ticket is issued,
`DidResume` is true, and `Used0RTT` is **false**.

## 3. The ordering rule the guard must follow

The guard is the one remaining place that dials with early data, so the rule
that removes the random failure still applies to it.

| # | Fact | Evidence |
|---|---|---|
| B1 | Inside quic-go, one part of the connection writes the peer's settings while another part reads them when a stream is opened. The field has no protection, so the two can collide. | `quic-go@v0.61.0` `connection.go:2375`, `:2935` |
| B2 | Both of those run on the same internal loop, and the "handshake finished" signal is sent **after** the write. Go guarantees that anything you do after receiving that signal sees the write. | `connection.go:2018-2030`, `:932`; Go memory model |
| B3 | ⟹ Waiting for the handshake signal before touching the connection removes the collision **entirely** — it does not merely make it rarer. | B1 + B2 |
| B4 | Exactly two calls break the rule: `OpenStreamSync` and `ConnectionState()`. Both read the unprotected field. | `connection.go:2935`, `:780` |
| B5 | Waiting for the handshake costs nothing: `Used0RTT` is set by the TLS handshake itself, not by writing on a stream. Measured true on both sides even with no stream opened at all. | `internal/handshake/crypto_setup.go:464,533`; 9/9 probe runs |

**The rule: on a connection dialled with `DialAddrEarly`, call nothing until
`HandshakeComplete()` has fired.**

## 4. Accepted risk

The QUIC tests stay in `internal/agentapi`, where 20 test files are set to run
at the same time. Go's race checker fails **every test running at that moment**,
so a future problem inside quic-go would again fail about twenty tests whose
names have nothing to do with QUIC — which is why the 2026-08-30 failure told
nobody anything. Deleting the offending test and following §3 removes the known
cause; it does not change what a future one would look like. Accepted
deliberately: moving the tests was considered and declined.

## 5. File inventory

| Path | Change |
|---|---|
| `server/internal/agentapi/quic_resumption_test.go` | delete `TestQUIC0RTT_ClientCertBehaviour`; clean the file header |
| `server/internal/agentapi/server_zero_rtt_test.go` | **new** — the listener guard |
| `docs/adr/ADR-037-client-first-fast-path-reconnect.md` | repoint the early-data evidence; name the guard |
| `.claude/techdebt.md` | **done 2026-08-31** — entry and its empty `## Severity: High` heading removed |
| `.claude/phases.md` | add a Completed row |
| `.claude/plans/archive/quic-0rtt-race-paydown.md` | archive this plan in the same commit |

Nothing else moves. `startResumeTestServer`'s `allow0RTT` parameter loses its
only `true` caller — collapse it rather than leave a switch nothing flips.

## 6. Steps

1. **Write `server/internal/agentapi/server_zero_rtt_test.go`** —
   `TestProductionListenerRefusesEarlyData`. Start `newAcceptEnv`; dial once
   normally to bank a session ticket using the existing `newSignalingCache` and
   `waitForTicket`; then dial with `DialAddrEarly`; **receive on
   `HandshakeComplete()` before touching the connection** (§3); then assert:
   - a session ticket was issued — reconnects can resume at all,
   - `DidResume` is true,
   - **`Used0RTT` is false** — the running listener refuses early data.
   A comment states the ordering rule directly, in its own words, with no
   reference to a decision number or plan name.
2. **Delete `TestQUIC0RTT_ClientCertBehaviour`** from `quic_resumption_test.go`.
3. **Clean the file header** of `quic_resumption_test.go`: drop the third bullet
   about early-data behaviour, drop the spike framing, and remove the hardcoded
   `v0.60.0` — the version belongs in `go.mod`, not in prose. Describe what the
   two remaining tests check, in the present tense.
4. **Collapse `allow0RTT`** in `startResumeTestServer` — no caller passes true
   any more.
5. **Edit ADR-037.** In the paragraph that says early data is kept off, name
   `TestProductionListenerRefusesEarlyData` as what holds it off. Where the file
   is cited as the empirical record (line ~142), make the archived W3 plan the
   record for the early-data finding and leave the test file as the record for
   resumption. Both existing links stay as they are — the file survives.
6. **`phases.md` Completed row** and **archive this plan** in the same commit:
   `git mv .claude/plans/quic-0rtt-race-paydown.md .claude/plans/archive/`, bump
   this file's own links one `../` deeper, `git add` the **new** path, and link
   `plans/archive/…` from the row. Row prose ≤300 characters.
7. `/precommit` → commit → `/refactor` → push.

## 7. Reviewer checklist

- [ ] `grep -rn "Allow0RTT" server/` returns **nothing**. The setting exists
      nowhere in the repo, and its absence is now asserted.
- [ ] `grep -rn "DialAddrEarly" server/` returns exactly one hit, and it is
      followed by a `HandshakeComplete()` receive before any `OpenStreamSync` or
      `ConnectionState()` on that connection.
- [ ] The guard **fails** when `Allow0RTT: true` is added to `server.go`'s
      `quic.Config`. **Verify by temporarily adding it** — an assertion nobody
      has watched fail is not yet a guard.
- [ ] No test asserts by `t.Logf` alone.
- [ ] No hardcoded quic-go version anywhere in the test text.
- [ ] No decision number, plan name or phase label in any new code comment.
- [ ] `go build ./... && go vet ./...` clean;
      `GO111MODULE=off go run ./scripts/check-doc-links` clean.
- [ ] `phases.md` row links `plans/archive/quic-0rtt-race-paydown.md` and the
      plan is at that path in the same commit.
- [ ] Gauntlet green.

## 8. Out of scope

- **Any replacement early-data test.** Nothing in OpenGate uses early data, so
  such a test would check a fact about the QUIC library rather than about this
  system. If early data is ever adopted, the test gets written then, beside the
  code that does it.
- **Moving the tests to another package.** Considered and declined; see §4.
- **Anything upstream.** No report, no comment, no patch on
  [quic-go#4303](https://github.com/quic-go/quic-go/issues/4303). §3 removes our
  exposure, the bug is already filed by someone else, and it has sat untouched
  for two and a half years. Reopen the question only if something later needs to
  use a connection before its handshake has finished.
- **A new ADR.** No decision changes — ADR-037 already holds it and is edited in
  place. No `decisions.md` row.
