# Micro-Plan: Agent Key Permissions + Server-Side Resumption Evidence

**Status:** **READY TO IMPLEMENT.** Every fact below is verified against the
working tree at the pinned toolchain, and every decision is locked. **Branch:**
`dev`. **Owner:** agent (Rust) + server (Go) + installer (Bash).
**Register entries paid:** [`techdebt.md`](../techdebt.md) — "The agent's
private key and data directory are world-readable" (High) and "Nothing says
whether a reconnecting agent resumed its TLS session" (Medium).
**Sibling:** the `td-agent-cross-restart-tls-resumption` micro-plan carries the
blocked half (surviving a process restart). Neither plan depends on the other.

This lands in **two commits** (D15): the code, and then the register close-out
after the live observation the Medium entry's trigger asks for.

Three gaps on the managed endpoint, all closable against the pinned toolchain:

1. The agent writes its mTLS **private key world-readable** (`0644`) into a
   world-traversable directory (`0755`) on every managed machine.
2. The installer writes the systemd unit **world-readable** with a **reusable
   enrollment token** in its `ExecStart` line.
3. The W3 resumption saving is **unobservable in production**. The register's
   own pay-down trigger asks for a reconnecting agent "observed resuming
   (`DidResume`)", and no such signal is exported anywhere.

---

## 1. Established facts

Each verified against the working tree at the pinned toolchain, or measured.

### Key, directory and unit-file permissions

| # | Fact | Evidence |
|---|---|---|
| F1 | `agent.key` is written by a bare `std::fs::write` with **no mode**, on **both** identity paths: `AgentIdentity::generate` and `PendingIdentity::generate`. | [`identity.rs:103`](../../agent/crates/mesh-agent-core/src/identity.rs#L103), [`identity.rs:148`](../../agent/crates/mesh-agent-core/src/identity.rs#L148) |
| F2 | The data directory is created with **no mode at three sites** — not one. | [`identity.rs:89`](../../agent/crates/mesh-agent-core/src/identity.rs#L89), [`identity.rs:135`](../../agent/crates/mesh-agent-core/src/identity.rs#L135), [`main.rs:377`](../../agent/crates/mesh-agent/src/main.rs#L377) |
| F3 | The production installer creates it first, also with no mode: `mkdir -p "$DATA_DIR"`. | [`install.sh:189`](../../server/internal/api/install.sh#L189) |
| F4 | **Measured** under the default `umask 022`: directory `0755`, key file `0644`. `create_dir_all` opens `0777 & ~umask`; `fs::write` opens `0666 & ~umask`. | Reproduced locally with the same syscall semantics |
| F5 | The service runs as **root** (`User=root`), so the key is a root-owned file that every local account on the endpoint can read. | [`install.sh:212`](../../server/internal/api/install.sh#L212) |
| F6 | `create_dir_all` on an **existing** directory returns `Ok` and **leaves its mode alone**. F3 means the directory usually exists before the agent runs, so a mode passed at creation time would never apply. | `std::fs` semantics; F3 |
| F7 | The in-repo precedent to copy is a `#[cfg(unix)]` block using `PermissionsExt`. | [`update.rs:123`](../../agent/crates/mesh-agent-core/src/update.rs#L123) |
| F8 | The agent ships **Linux only** (`x86_64-unknown-linux-musl`, `aarch64-unknown-linux-musl`) and no `cfg(windows)` exists anywhere in `agent/crates`. `#[cfg(unix)]` therefore covers everything shipped. | [`release-agent.yml:52-55`](../../.github/workflows/release-agent.yml#L52-L55); repo grep |
| F9 | The test-skip guard bans `#[ignore]` only, so a `#[cfg(unix)]`-gated test is permitted and always runs on CI. | [`pretooluse-test-skip-guard.sh:49`](../hooks/pretooluse-test-skip-guard.sh#L49) |
| F10 | `identity.rs` already carries 8 unit tests using `tempfile::tempdir()`. | [`identity.rs`](../../agent/crates/mesh-agent-core/src/identity.rs) |
| F21 | `AgentIdentity::generate` runs whenever **any one** of the three identity files is missing — `load_or_create` requires all three — so it can reach a data directory that still holds a stale `agent.key`. A partial uninstall (`remove_identity_files`) or an interrupted enrollment leaves exactly that state. | [`identity.rs:50`](../../agent/crates/mesh-agent-core/src/identity.rs#L50), [`main.rs:1178`](../../agent/crates/mesh-agent/src/main.rs#L1178) |
| F22 | The installer writes the systemd unit with a bare `cat >` — `0644` under the default umask — and its `ExecStart` carries `--enroll-token`. Enrollment tokens are **multi-use with an expiry**: the server refuses one only once exhausted or expired. So a live credential for enrolling machines is readable by every local account on the endpoint. | [`install.sh:198`](../../server/internal/api/install.sh#L198), [`handlers_enrollment.go:164`](../../server/internal/api/handlers_enrollment.go#L164) |
| F23 | A shell test already asserts the installer's content, and is the place a mode assertion goes. | [`install-sh.test.sh`](../../scripts/tests/install-sh.test.sh) |
| F28 | `update-signing-key.hex`, the other file the agent writes into the data directory, is the server's **verifying** key — public by nature. The `0700` directory is all it needs; it gets no `0600`. | [`main.rs:141`](../../agent/crates/mesh-agent/src/main.rs#L141), [`update.rs`](../../agent/crates/mesh-agent-core/src/update.rs) |
| F29 | Rust mutation shards are **explicit file lists**, `identity.rs` is already in one, and nothing enforces Rust partition completeness the way the Go table is enforced. A new Rust module file would silently fall outside mutation coverage. | [`mutation-shards.sh:153`](../../scripts/lib/mutation-shards.sh#L153), [`mutation-shard-budget.test.sh`](../../scripts/tests/mutation-shard-budget.test.sh) |

### Resumption evidence

| # | Fact | Evidence |
|---|---|---|
| F11 | `AgentServer` **already holds** `metrics *appmetrics.Metrics`, and `AgentServerConfig` already carries the field. No plumbing needed. | [`server.go:46`](../../server/internal/agentapi/server.go#L46), [`server.go:70`](../../server/internal/agentapi/server.go#L70) |
| F12 | `performHandshake` **already reads** `conn.ConnectionState().TLS`. `DidResume` is one field access away. | [`server_connection.go:199`](../../server/internal/agentapi/server_connection.go#L199) |
| F13 | `accept` calls `performHandshake` **unconditionally**, and the `0x14` fast path branches *inside* the handshaker — so a counter placed there sees the fast path as well as full `AgentHello` handshakes. | [`server_connection.go:38`](../../server/internal/agentapi/server_connection.go#L38), [`handshaker.go:70`](../../server/internal/agentapi/handshaker.go#L70) |
| F27 | `performHandshake` runs **after** `acceptControlStream`, so the counter's population is *connections that reached the application handshake* — a connection lost before its control stream opens is not counted. | [`server_connection.go:33-41`](../../server/internal/agentapi/server_connection.go#L33) |
| F14 | A real loopback QUIC listener with the product's own mTLS config is available in-process — but `newAcceptEnv` builds `NewAgentServer` with **no `Metrics`**, and `acceptEnv.dial` builds its client config internally with **no `ClientSessionCache`** and signs a **fresh certificate per call**. Neither the counter nor a shared ticket is reachable without extending the harness. | [`server_accept_test.go:55`](../../server/internal/agentapi/server_accept_test.go#L55), [`server_accept_test.go:88`](../../server/internal/agentapi/server_accept_test.go#L88) |
| F15 | A cold-then-resumed dial pair that demonstrably flips `DidResume` exists, but is bound to its own listener: `agentResumeTLSConfig` sets `NextProtos: resumeTestALPN`, while the real listener speaks `opengate`. Its `newSignalingCache` and `waitForTicket` are in the same package and **are** reusable. | [`quic_resumption_test.go:155-166`](../../server/internal/agentapi/quic_resumption_test.go#L155), [`cert.go:159`](../../server/internal/cert/cert.go#L159) |
| F26 | Resumption today spans **in-process reconnects only** — a process restart comes back cold (the sibling plan's gap). A production observation of `resumed="true"` must therefore be driven by an in-process redial: bouncing the **server**, not the agent. | [`techdebt.md`](../techdebt.md), "Cross-restart TLS resumption is blocked on a rustls release" |
| F16 | The metric-assertion pattern is established: `appmetrics.NewMetrics(prometheus.NewRegistry())` plus `prometheus/testutil`. | [`conn_register_metrics_test.go`](../../server/internal/agentapi/conn_register_metrics_test.go) |
| F17 | An **agent-side** store counter would be dishonest: rustls takes a ticket and *may then reject it* at `compatible_config` / `has_expired`, so `take_tls13_ticket() == Some` over-counts. That gate is live on the pinned `rustls 0.23.43`. | `rustls-0.23.43` `src/msgs/persist.rs:254-278` |
| F18 | quinn exposes **no** client-side resumption signal — `handshake_data()` yields ALPN and SNI only. Server-side is the only faithful observation point available. | `quinn-proto-0.11.15` `src/crypto/rustls.rs:259-271` |
| F30 | The nil-guard convention this plan follows is real and in force in the package. | [`conn_accounting.go:53`](../../server/internal/agentapi/conn_accounting.go#L53) |
| F19 | No mutation-shard change is needed on the Go side: `server_connection.go` is in `go-agentapi-connection`, and `internal/metrics` is covered by a `dir:` shard. | [`mutation-shards.sh:336,376`](../../scripts/lib/mutation-shards.sh#L336) |
| F20 | `sonar.coverage.exclusions` lists **no production file**, so every touched file is measured by the coverage gate. | [`sonar-project.properties:41-47`](../../sonar-project.properties#L41) |

## 2. Locked decisions

| # | Decision |
|---|---|
| D1 | **The agent owns the data-directory permissions.** The agent creates the directory itself on three paths (F2) and must be correct on a machine where the installer never ran (container, `OPENGATE_DATA_DIR` override). Hardening only the installer would leave every one of those uncovered. |
| D2 | **The key is created `0600` atomically**, via `OpenOptions::new().mode(0o600)` — **not** write-then-`chmod`. A write-then-chmod leaves the private key world-readable for a real interval. |
| D3 | **The directory is repaired, not just created.** Because `create_dir_all` leaves an existing directory's mode alone (F6) and the installer usually gets there first (F3), the agent calls `set_permissions(0o700)` explicitly after ensuring the directory exists. Creating it with a mode is not sufficient on its own. |
| D4 | **One helper, used by every site.** The three directory sites (F2) and the two key sites (F1) each call one shared helper, so a fourth call site inherits the mode instead of re-introducing the gap. |
| D5 | **`#[cfg(unix)]`, no Windows branch.** F8 — nothing else ships. A `cfg`-gated test compiles out rather than skipping at runtime, so it does not trip the skip guard (F9). |
| D9 | **No load-path repair for existing keys.** The fleet is one machine and it is reinstalled from scratch, so every `agent.key` in existence once this lands was created by the fixed code. `load_or_create`'s load branch is not touched. |
| D10 | **`create(true).truncate(true)`, never `create_new(true)`.** F21 puts a stale `agent.key` in front of `generate` on a real path, and `create_new` would fail there with `AlreadyExists` and stop the agent from starting. The helper then asserts the mode with one `set_permissions(0o600)` once the file exists — which is not the write-then-chmod D2 bans: the mode is already correct at creation, and the assert only bites the stale-file case, so the key is never observably `0644`. |
| D11 | **The helper lives in `identity.rs`, is sync, and is re-exported from `lib.rs`.** A new module file would fall outside every Rust mutation shard (F29). `main.rs` calls it in place of its `tokio::fs::create_dir_all` — a single blocking `mkdir` at start-up is free. |
| D14 | **The installer writes the unit `0600`** (F22). D1 keeps the *data directory* the agent's job; the unit file is the installer's own output and only the installer can own it. |
| D6 | **The resumption signal is the server-observed `DidResume`.** Not an agent-side store counter (F17), not a client-side handshake kind (F18). |
| D7 | **Metric shape:** `opengate_agent_tls_handshakes_total{resumed="true"\|"false"}` — one counter vec, observed once per connection. A vec rather than two counters, so the resumption *ratio* is a single query with its own denominator. |
| D12 | **Observed at the top of `performHandshake`**, right after `tlsState` is read and before the application handshake can fail — the series then counts TLS handshakes rather than successful registrations. The bound in F27 is accepted and stated in the metric's help text: a connection lost before its control stream opens is not counted. |
| D13 | **The accept-test harness is extended, not duplicated.** `newAcceptEnv` gains a variant carrying a `*appmetrics.Metrics`; `dial` gains a form taking a client `*tls.Config` and a fixed device ID, with the existing `dial` delegating to it so no current test changes behaviour. The new test builds its config from `cert.AgentTLSConfig` (ALPN `opengate`, F15) plus `newSignalingCache`, and waits with `waitForTicket`. |
| D15 | **Two commits.** Commit 1 lands the code, the tests and the docs. The deploy reaches production through the normal merge-to-main path; the live observation (step 10) then runs, and commit 2 deletes both register entries, adds the ledger row and archives this plan. Both commits run `/precommit`. |
| D16 | **The live observation is driven by bouncing the server.** Restarting the *agent* would produce `resumed="false"` by construction (F26) and prove nothing; a server-side bounce makes the running agent redial in process, which is exactly the saving ADR-037 claims. |
| D8 | **0-RTT stays off.** Out of scope here and everywhere until a replay-safety ADR exists. |

## 3. Scope boundary — what this plan does *not* do

- **Cross-restart persistence.** A restarted agent still starts with an empty
  in-memory ticket store and does a full handshake. That is the sibling
  `td-agent-cross-restart-tls-resumption` micro-plan, and it is blocked upstream.
- **Repairing keys already on disk.** D9 — the fleet is reinstalled.
- **Enabling resumption.** It is already on: `rustls::ClientConfig` defaults
  `resumption` to `in_memory_sessions(256)`, and the agent builds the quinn
  config once ([`main.rs:432`](../../agent/crates/mesh-agent/src/main.rs#L432))
  and clones it per attempt
  ([`main.rs:599`](../../agent/crates/mesh-agent/src/main.rs#L599)); the clone
  shares `crypto: Arc<dyn ClientConfig>`, so one session store spans reconnects.
  This plan **measures** that, it does not switch it on.
- **Changing how the enrollment token reaches the unit.** D14 closes who can
  read the file; moving the token out of `ExecStart` into an environment file is
  a design change to enrollment and is not taken here.
- **0-RTT**, a client-side resumption signal, or any change to
  `Allow0RTT` server-side.

## 4. File inventory

| File | Change |
|---|---|
| [`agent/.../identity.rs`](../../agent/crates/mesh-agent-core/src/identity.rs) | Shared perms helper (D10, D11); `0600` key creation on both paths; `0700` directory on both paths |
| [`agent/.../lib.rs`](../../agent/crates/mesh-agent-core/src/lib.rs) | `pub use` the helper |
| [`agent/.../main.rs`](../../agent/crates/mesh-agent/src/main.rs) | The data-directory creation site calls the helper |
| [`server/internal/api/install.sh`](../../server/internal/api/install.sh) | Systemd unit written `0600` (D14) |
| [`scripts/tests/install-sh.test.sh`](../../scripts/tests/install-sh.test.sh) | Assert the unit's mode |
| [`server/internal/metrics/metrics.go`](../../server/internal/metrics/metrics.go) | New counter vec + `ObserveAgentTLSHandshake(resumed bool)` |
| [`server/internal/metrics/metrics_test.go`](../../server/internal/metrics/metrics_test.go) | Registration + both label values |
| [`server/.../server_connection.go`](../../server/internal/agentapi/server_connection.go) | Observe `tlsState.DidResume` at the top of `performHandshake`, behind the `s.metrics != nil` convention (F30) |
| [`server/.../server_accept_test.go`](../../server/internal/agentapi/server_accept_test.go) | Metrics-carrying env variant + config-taking `dial` form (D13) |
| `server/.../server_accept_metrics_test.go` (new) | Cold and resumed dials assert the counter |
| [`docs/infrastructure/Monitoring.md`](../../docs/infrastructure/Monitoring.md) | One metric row + the ratio query |
| [`.claude/techdebt.md`](../techdebt.md) | Commit 2: delete the two entries this pays |
| [`.claude/phases.md`](../phases.md) | Commit 2: Completed row |
| This plan | Commit 2: `git mv` to `archive/`, links bumped one `../` deeper |

## 5. Steps

Test first at every step — the gate blocks the first source edit otherwise.

### Commit 1 — the code

1. **Rust test.** In `identity.rs` tests, assert `0600` on `agent.key` and
   `0700` on the data dir, for `AgentIdentity::generate` **and**
   `PendingIdentity::generate`, under `#[cfg(unix)]`. Add the two cases that
   actually bite: a temp dir pre-created `0755` (what the installer leaves,
   F3/F6) is repaired to `0700`, and a stale `agent.key` already present (F21)
   is rewritten rather than erroring, ending at `0600`. Confirm red.
2. **Rust source.** Add the helper to `identity.rs`, re-export it, and wire all
   five sites plus `main.rs` (D2, D3, D4, D10, D11). Confirm green.
3. **Installer test.** In `install-sh.test.sh`, assert the unit file is written
   `0600`. Confirm red.
4. **Installer source.** Write the unit with the mode set at creation (D14).
   Confirm green.
5. **Go test — metric.** In `metrics_test.go`, assert the new collector
   registers and carries both `resumed` label values. Confirm red.
6. **Go source — metric.** Add the counter vec and `ObserveAgentTLSHandshake`
   to `metrics.go`, following the existing `counterVec` helper. Its help text
   states the population (F27). Confirm green.
7. **Go test — observation.** Extend the accept-test harness per D13, then add
   `server_accept_metrics_test.go`: dial cold, assert `resumed="false"`
   incremented by one; wait for the ticket, dial again on the same client
   config and device ID, assert `resumed="true"` incremented by one. Confirm
   red.
8. **Go source — observation.** Observe `tlsState.DidResume` at the top of
   `performHandshake` (D12), guarded by `s.metrics != nil`. Confirm green.
9. **Docs.** Add the metric row and the ratio query to `Monitoring.md`. Live
   state only — describe what the series reports, not what was missing before.
   Then `/precommit` → commit → `/refactor` → push.

### Commit 2 — the close-out, after the deploy lands

10. **Live observation.** Once the change is running in production, bounce the
    server so the live agent redials in process (D16), then query the cluster's
    Prometheus for `sum by (resumed) (increase(opengate_agent_tls_handshakes_total[1h]))`
    and require a non-zero `resumed="true"`. That is the second half of the
    Medium entry's pay-down trigger, and the first time the ADR-037 saving is
    measured rather than asserted.
11. **Register.** Delete both `techdebt.md` entries this plan pays. Leave the
    cross-restart entry alone — it is the sibling plan's.
12. **Ledger + archive, same commit.** `phases.md` Completed row, `git mv` this
    plan to `archive/`, bump its internal links one `../` deeper, validate with
    `GO111MODULE=off go run ./scripts/check-doc-links`, and re-`git add` the
    **new** path.

## 6. Reviewer checklist

- [ ] Key is `0600` and directory `0700` after `AgentIdentity::generate`.
- [ ] Same after `PendingIdentity::generate` — the enrollment path is not a
      second-class path; it writes the same private key.
- [ ] A directory pre-created `0755` is **repaired**, not left alone (D3). This
      is the case that reproduces production, and the one a naive fix misses.
- [ ] A stale `agent.key` in front of `generate` (F21) is rewritten at `0600`,
      not refused — no `create_new` anywhere (D10).
- [ ] The key is never observably `0644` — creation carries the mode (D2).
- [ ] Every directory creation site from F2 goes through the helper (D4), and
      the helper sits in `identity.rs` rather than a new unsharded file (D11, F29).
- [ ] No `#[ignore]`; `#[cfg(unix)]` only (F9).
- [ ] The systemd unit is `0600`, asserted by the shell test (D14, F22).
- [ ] The counter increments on the `0x14` fast path too — not only on full
      `AgentHello` handshakes (F13).
- [ ] Both label values are exercised by a test that drives a **real** QUIC
      handshake against the product's own listener, not a hand-built
      `tls.ConnectionState` and not the resume-test listener (D13, F15).
- [ ] The existing `dial` callers are unchanged in behaviour by the harness
      extension (D13).
- [ ] `Monitoring.md` gained the row; no doc narrates the previous absence.
- [ ] The live check was driven by a **server** bounce, not an agent restart
      (D16) — an agent restart proves nothing here.
- [ ] `techdebt.md` lost exactly the two paid entries; the cross-restart entry
      survives untouched.
- [ ] This plan is in `archive/` in the commit that closes it, with its links
      bumped and the new path staged.
