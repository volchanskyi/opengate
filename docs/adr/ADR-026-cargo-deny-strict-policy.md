# ADR-026: cargo-deny strict policy — HTTP-stack inventory, transitive-duplicate allowlist, ADR-020 §5.4 flip

Date: 2026-05-29
Status: Accepted

## Context

[ADR-020](ADR-020-modular-monolith-full-hexagonal.md) §5.2 set up `cargo-deny` as the Rust enforcement gate for the modular-monolith effort, with three open items deferred to a follow-on ADR:

1. **HTTP-stack inventory.** §5.2 envisions: "no crate may add new external HTTP deps without ADR amendment." Until the inventory exists, the ban list is empty.
2. **Multiple-versions / wildcards severities.** Both started at `"warn"` per ADR-020 §5.4's auto-flip rule — flip from warn to error when the violation count reaches zero. Until something resolves the 28 pre-existing duplicate warns and 7 wildcard warns, the flip cannot fire.
3. **Marker for the flip.** ADR-020 §5.4 specifies a per-gate marker under [`.claude/.markers/arch-lint-flipped/`](../../.claude/.markers/arch-lint-flipped/) recording the event. The script [`scripts/arch-lint-flip.sh`](../../scripts/arch-lint-flip.sh) needs to learn the cargo-deny gate's state machine.

This ADR closes all three.

## Decision

### HTTP-stack inventory (amends ADR-020 §5.2)

The Rust agent's complete inbound HTTP surface at the time of this ADR:

| Crate | Role | Direct workspace dep? | Pulled in by |
|---|---|---|---|
| `reqwest` | HTTP client for agent-update manifest fetch + GitHub manifest sync | yes | `mesh-agent`, `mesh-agent-core` |
| `tokio-tungstenite` | WebSocket client for signaling-related future work | yes | `mesh-agent-core` |
| `hyper`, `hyper-rustls`, `hyper-util` | HTTP/1 + HTTP/2 transport | no (transitive) | `reqwest` |
| `http`, `http-body`, `http-body-util` | HTTP types | no (transitive) | `reqwest`, `hyper` |
| `tungstenite` | sync WebSocket primitives | no (transitive) | `tokio-tungstenite` |

**Policy:** adding a new *direct* HTTP crate (`ureq`, `isahc`, `surf`, `attohttpc`, `hreq`, `awc`, etc.) to any workspace `Cargo.toml` requires an ADR amendment before merge. The inventory in this section is the canonical allowlist.

**Enforcement caveat:** `cargo-deny` cannot express "direct-only" bans — `[bans.deny]` rejects any presence in the dependency graph, transitive or not. Enforcement of the no-new-direct-HTTP-crate rule is therefore via code review on `agent/crates/*/Cargo.toml` diffs plus this ADR as the policy doc. The one explicit `[bans.deny]` entry below covers a crate (`openssl`) we want kept out even transitively.

```toml
[[bans.deny]]
crate = "openssl"
reason = "rustls-only policy — openssl pulls system-level crypto and complicates static cross-compilation."
```

### Transitive-duplicate allowlist (amends ADR-020 §5.4)

The 28 pre-existing `multiple-versions` warns are pinned by upstream version stagger across:

- **RustCrypto stack split (0.10/0.11 era)** — `block-buffer`, `cpufeatures`, `crypto-common`, `digest`, `sha2`, `const-oid`. Older versions pulled in via `reqwest`'s TLS path; newer via `rcgen 0.14` and direct mTLS code.
- **rand / getrandom split** — `rand_core`, `getrandom` (three majors: 0.2, 0.3, 0.4), `r-efi`. Multiplexed across `ring`, `rustls`, `webrtc-rs`.
- **rcgen split** — `rcgen 0.13` (via `webrtc-rs` + `dtls`) vs `rcgen 0.14` (direct mTLS), pulling `yasna 0.5` vs `yasna 0.6` as a paired duplicate.
- **Windows API binding cohort** — `windows-sys` (0.45/0.52/0.61), `windows-targets` (0.42/0.52), and the eight `windows_<arch>_<abi>` subtargets, pulled in by `parking_lot`, `socket2`, `getrandom`, `mio`.
- **Single-version slack** — `bitflags` (1↔2), `nix` (0.26↔0.28), `nom` (7↔8), `thiserror` (1↔2), `webpki-roots` (0.26↔1.0), `hashbrown`, `cfg_aliases`, `socket2`.

Each is recorded in [`agent/deny.toml`](../../agent/deny.toml) `[bans.skip]` with an `@<old-version>` spec and a `reason` string identifying the cause. The reviewer-facing rule: when an upstream bump resolves a duplicate, drop the corresponding `skip` entry. This list drifts down only.

### Wildcard handling (amends ADR-020 §5.4)

The 7 `wildcards` warns are workspace-internal path deps (`mesh-agent → mesh-agent-core`, `mesh-agent-core → mesh-protocol`, `platform-linux → mesh-agent-core`, `platform-windows → mesh-agent-core` + `mesh-protocol`) using `version = "*"`. They are not external CVE vectors.

Two changes resolve the warns without weakening the gate:

1. **`publish = false`** added to all five workspace crates (`mesh-agent`, `mesh-agent-core`, `mesh-protocol`, `platform-linux`, `platform-windows`). These crates ship via GitHub Releases (per [ADR-005](ADR-005-agent-auto-update.md) + the agent-binary-release pipeline), never to crates.io.
2. **`allow-wildcard-paths = true`** added to `[bans]` in [`agent/deny.toml`](../../agent/deny.toml). Cargo-deny applies this exemption only to crates marked `publish = false`, so change (1) is a prerequisite for change (2).

### Gate flip (executes ADR-020 §5.4)

`multiple-versions` and `wildcards` both flip `warn → deny` in [`agent/deny.toml`](../../agent/deny.toml). The marker [`.claude/.markers/arch-lint-flipped/cargo-deny`](../../.claude/.markers/arch-lint-flipped/cargo-deny) records the flip event. [`scripts/arch-lint-flip.sh`](../../scripts/arch-lint-flip.sh) gains a state-aware cargo-deny gate (same shape as the eslint-boundaries gate from the prior commit): `flipped` when marker present OR both severities at `deny`; `eligible` when both severities at `warn` and no marker; `no config` when [`agent/deny.toml`](../../agent/deny.toml) is missing; `--apply` atomically mutates both severities and writes the marker. Reconcile branch handles the case where the config was edited to `deny` in the same commit that wires the gate.

## Consequences

- Any net-new transitive dependency that introduces a duplicate failed the previous warn ratchet and now fails CI loudly. Resolving requires either bumping the direct dep that pulls in the conflict OR adding a `skip` entry with a documented reason — same review surface as a Sonar suppression.
- The 28-entry skip list is the durable maintenance surface. Reviewer rule: each skip is an IOU; every dependency bump should be reviewed against this list for entries that can be dropped.
- `publish = false` on the five workspace crates is a behavioral change in surface (`cargo publish` against any of them now fails by default) but matches the actual distribution model. The agent has never been published to crates.io.
- Three of four arch-lint gates now flipped: `depcruise` (commit `25bf0c9`), `eslint-boundaries` (commit `01856ed`), `cargo-deny` (this commit). `go-arch-lint` and `cargo-modules` were already strict at gauntlet adoption.
- The HTTP-stack inventory becomes the canonical reference for code review on `Cargo.toml` diffs. Adding `ureq` etc. as a direct dep without an ADR amendment is now a documented policy violation, not a vague preference.

## Follow-up work

None ADR-required. Two opportunistic follow-ups when convenient:

- Bump direct deps to `thiserror 2.x` to resolve the thiserror 1↔2 stagger; the skip entry can then be dropped.
- Audit the Windows API binding cohort when `webrtc-rs` next bumps; multiple skips likely collapse.

## References

- [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) §5.2 (Rust enforcement tooling), §5.4 (warn→error auto-flip mechanism)
- [`agent/deny.toml`](../../agent/deny.toml) — strict policy implementation
- [`scripts/arch-lint-flip.sh`](../../scripts/arch-lint-flip.sh) — flip script state machine
- [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) — `cargo deny check` gauntlet steps
- [cargo-deny docs](https://embarkstudios.github.io/cargo-deny/) — `skip`/`deny`/`allow-wildcard-paths` reference
