# Architecture Decision Records

<!-- Last updated: 2026-03-30 -->
<!-- Compact index — full ADR text lives in the wiki: -->
<!-- https://github.com/volchanskyi/opengate/wiki/Architecture-Decision-Records -->
<!-- Add a new row here AND a full section in the wiki page for each new ADR. -->

| ADR | Decision | Phase | Status |
|-----|----------|-------|--------|
| 001 | MessagePack wire protocol with internally tagged enums, `[type][len][payload]` framing | 1 | Accepted |
| 002 | Golden file tests — Rust generates, Go verifies | 1 | Accepted |
| 003 | SQLite WAL via `modernc.org/sqlite` (pure Go, no CGo), `MaxOpenConns(1)` | 2 | Accepted |
| 004 | ECDSA P-256 self-signed CA, CSR enrollment via `/api/v1/enroll/{token}`, TLS 1.3 | 2 | Accepted |
| 005 | QUIC mTLS via quic-go — server opens control stream (workaround) | 4 | Accepted (workaround) |
| 006 | Platform traits with null impls for headless/CI environments | 5 | Accepted |
| 007 | VAPID Web Push, keypair persisted to `{dataDir}/vapid.json` | 10 | Accepted |
| 008 | `aarch64-unknown-linux-musl` cross-compilation via `cross` | CD-C | Accepted |
| 009 | Cosign keyless signing for container images (GitHub OIDC) | CD-E | Accepted |
| 010 | Separate `device_hardware` table for on-demand hardware inventory; `RestartAgent`/`RequestHardwareReport`/`HardwareReport` control messages | 12+ | Accepted |
| 011 | On-demand device logs via control path (`RequestDeviceLogs`/`DeviceLogsResponse`/`DeviceLogsError`), individual rows in `device_logs` table with 5-min cache TTL, SQL-level filtering | — | Accepted |
