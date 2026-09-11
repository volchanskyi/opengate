# Architecture Decision Records

The early decisions, kept together because each is a paragraph. Everything from
ADR-014 onward is a file of its own under [`adr/`](./adr/). The index is
[`.claude/decisions.md`](https://github.com/volchanskyi/opengate/blob/dev/.claude/decisions.md).

Every record describes what the system does now. When a decision changes, the
record changes with it.

---

## ADR-001: MessagePack wire protocol

MessagePack, with internally tagged enums in Rust (`#[serde(tag = "type")]`) and
matching struct tags in Go. Frames are `[1-byte type][4-byte big-endian length][payload]`;
ping and pong are a single byte. The handshake uses raw binary rather than
MessagePack, on type bytes `0x10`–`0x15`.

JSON is too verbose for a per-second telemetry path, protobuf needs a schema
compiler in both toolchains, and a bespoke format is a maintenance cost forever.
MessagePack is compact, needs no schema, and both languages have a good library.

---

## ADR-002: Golden files prove the two languages agree

Two independent implementations of one protocol drift silently. Rust generates
canonical binary fixtures in `/testdata/golden/` and Go tests decode them.
Go-to-Rust fixtures sit beside them, so the contract is checked in both
directions, and each fixture carries a `.meta.json` sidecar recording what it is
meant to contain — a fixture that is regenerated wrongly then fails against its
own description rather than quietly becoming the new expectation.

Run `make golden` after any protocol change. Fixtures are committed.

---

## ADR-004: ECDSA P-256 mutual TLS, with enrollment by signing request

The server is its own certificate authority, on a ten-year P-256 certificate.
An agent enrolls by posting a PKCS#10 signing request to
`/api/v1/enroll/{token}`; the server signs it and returns the certificate and
its own. TLS 1.3 minimum. A device registers without a group —
`devices.group_id` is nullable.

Mutual authentication without depending on a public certificate authority,
because agents run on untrusted networks. Enrollment tokens are issued by an
administrator and used once. Losing the authority key means re-enrolling every
agent.

---

## ADR-006: Platform traits, with null implementations

Agent capabilities are Rust traits: `ScreenCapture` is asynchronous and
dynamically dispatched, `InputInjector` and `ServiceLifecycle` are synchronous
and object-safe. The agent implements Linux; every trait a platform does not
implement resolves to its null implementation, which is what headless hosts,
containers and CI runs use.

An agent must not fail to start because a machine has no display.

---

## ADR-007: Web push, with the keypair in the data directory

The server generates a VAPID keypair on first run and persists it to
`{dataDir}/vapid.json`. A `Notifier` interface keeps push out of the HTTP
handlers.

Standard web push with no third-party push service. Changing the keys
invalidates every existing browser subscription, so the file is mounted from the
same Secret as the other server keys
([ADR-034](adr/ADR-034-server-keys-from-a-secret.md)).

---

## ADR-008: Cross-compiling the agent to aarch64 musl

The Rust agent cross-compiles to `aarch64-unknown-linux-musl` with `cross`; Go
builds for ARM64 natively. Both produce static binaries.

The deployment target is ARM64 and the CI runners are x86-64. Docker is
therefore required in CI. SELinux hosts need the replaced binary relabelled
after an over-the-air update, or it will not execute.

---

## ADR-009: Container images are signed without keys

Keyless signing through GitHub's OIDC identity and Sigstore. Deploys verify the
signature before doing anything, and a software bill of materials is attached to
the image.

Supply-chain assurance with no signing key to store or rotate; the signature
ties an image to the pipeline that built it. Verification adds a few seconds to
a deploy, and a failed verification stops it.

---

## ADR-010: Hardware inventory is its own table, collected on demand

Hardware lives in `device_hardware`, one row per device, cascading on delete.
Three control messages drive it: `RestartAgent`, `RequestHardwareReport` and
`HardwareReport`, the last carrying processor, memory, disk and network
interfaces.

Hardware is large, changes rarely, and is unrelated to what a device list needs,
so keeping it out of the `devices` row keeps every device query small. Collected
on demand rather than periodically, because a fleet of hundreds would otherwise
spend bandwidth reporting the same facts.

`GET /api/v1/devices/{id}/hardware` returns what is stored, or `202` while it
asks. `POST /api/v1/devices/{id}/restart` restarts the agent through exit code
42, which systemd is configured to treat as a restart.

---

## ADR-012: The SonarCloud gate blocks the merge

A required CI job aggregates coverage from the Go, Rust and web test jobs, runs
the pinned scanner over the whole codebase, and waits for the quality gate to
resolve. Findings live in the SonarCloud console rather than GitHub Code
Scanning, because fingerprint matching there treated dismissed findings as
already seen and kept new ones invisible.

The gate's conditions apply to new code only: coverage at or above 80%, an A
rating for reliability, security and maintainability, every new security hotspot
reviewed, and duplication under 3%.

Overall coverage is held separately, by each language's own test job failing
below 80%, and the pre-commit run reproduces all of it. So three independent
layers together hold both the whole codebase and each change to it, even though
the gate itself only measures the change.

Per-language thresholds alone miss what SonarCloud measures and `go test -cover`
does not — duplication, cognitive complexity, security hotspots. Running it as a
hard gate forces fixes on `dev` rather than letting them pile up on `main`.
