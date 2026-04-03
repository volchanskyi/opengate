# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.21.1] - 2026-04-03

### Fixed
- resolve log fetch crash and upgrade button issues, add bulk upgrade

## [v0.21.0] - 2026-04-03

### Added
- redesign device detail UI with upgrade button and side-by-side logs

## [v0.20.0] - 2026-04-02

### Added
- add on-demand device log retrieval via QUIC control path

## [v0.19.1] - 2026-04-02

### Fixed
- use camelCase noDataState in Grafana alert provisioning

## [v0.19.0] - 2026-04-02

### Added
- add /observe skill for autonomous diagnostics

## [v0.18.2] - 2026-04-01

### Fixed
- sync go.sum with go.mod for modernc.org/sqlite v1.47.0

## [v0.18.1] - 2026-03-31

### Fixed
- tune k6 scenarios to stay within per-IP rate limit

## [v0.18.0] - 2026-03-31

### Added
- add agent restart and on-demand hardware inventory

## [v0.17.1] - 2026-03-30

### Fixed
- hide Desktop and Chat tabs when capabilities are unknown

## [v0.17.0] - 2026-03-27

### Added
- system tray with IPC for desktop agent management

### Fixed
- exclude tray crate from headless CI, add dedicated desktop CI job

## [v0.16.1] - 2026-03-27

### Fixed
- correct agent online/offline status misreporting across all layers

## [v0.16.0] - 2026-03-26

### Added
- X11 screen capture with JPEG encoding + chat echo

## [v0.15.8] - 2026-03-26

### Fixed
- show error toast when Start Session fails silently

## [v0.15.7] - 2026-03-26

### Fixed
- restore SELinux context after OTA binary replacement

## [v0.15.6] - 2026-03-26

### Fixed
- probe display sockets instead of checking existence

### Changed
- extract probe_socket helper and deduplicate display tests

## [v0.15.5] - 2026-03-26

### Fixed
- agent startup ordering and version auto-sync from git tags

## [v0.15.4] - 2026-03-26

### Fixed
- detect display server via socket probing for systemd services

## [v0.15.3] - 2026-03-26

### Fixed
- stop capture loop after 3 consecutive failures instead of spamming

## [v0.15.2] - 2026-03-26

### Fixed
- remove invalid -rule flag from VictoriaMetrics single-node config

## [v0.15.1] - 2026-03-26

### Fixed
- OTA push returns 0 due to OS/arch mismatch between agent and manifest

## [v0.15.0] - 2026-03-26

### Added
- file manager view/download and capability-based session tabs

## [v0.14.4] - 2026-03-25

### Fixed
- npm deps bump

## [v0.14.3] - 2026-03-25

### Fixed
- tie agent version to git release tags instead of stale Cargo.toml

## [v0.14.2] - 2026-03-25

### Fixed
- use sudo for cosign install on VPS

## [v0.14.1] - 2026-03-25

### Fixed
- verify cosign signature before stopping containers and auto-install cosign on VPS

## [v0.14.0] - 2026-03-25

### Added
- add cosign keyless signing, SBOM attestation, and deploy-time verification

## [v0.13.14] - 2026-03-25

### Fixed
- add artifact retention policy and replace silent error discards

## [v0.13.13] - 2026-03-25

### Fixed
- use latest tag for Trivy image scan instead of full SHA

## [v0.13.12] - 2026-03-25

### Fixed
- replace all silent skip patterns in Makefile with loud failures

## [v0.13.11] - 2026-03-25

### Fixed
- resolve CI codegen check path issue and remove silent skip patterns
- use direct oapi-codegen invocation for codegen sync check
- install oapi-codegen in CI before codegen sync check

## [v0.13.10] - 2026-03-24

### Fixed
- wrap monitoring deploy comment to satisfy yamllint line-length rule
- resolve Grafana provisioning crashes and datasource UID mismatches

## [v0.13.9] - 2026-03-24

### Fixed
- quote Telegram chat ID to prevent Grafana provisioning crash

## [v0.13.8] - 2026-03-23

### Fixed
- implement http.Hijacker on metrics statusWriter to unblock WebSocket upgrades

## [v0.13.7] - 2026-03-23

### Fixed
- relay pipe context, ungrouped device access, and device listing query

## [v0.13.6] - 2026-03-23

### Fixed
- preserve WebSocket message boundaries in relay

## [v0.13.5] - 2026-03-23

### Fixed
- replace ReadTimeout/WriteTimeout with ReadHeaderTimeout for WebSocket compatibility

## [v0.13.4] - 2026-03-23

### Fixed
- pass Telegram env vars to Grafana container

## [v0.13.3] - 2026-03-23

### Fixed
- use container name for Uptime Kuma in Caddyfile

## [v0.13.2] - 2026-03-23

### Fixed
- skip /metrics smoke test in staging and production

## [v0.13.1] - 2026-03-23

### Fixed
- inject .env.monitoring via CD pipeline from GitHub Secrets

## [v0.13.0] - 2026-03-23

### Added
- add Phase D monitoring & observability stack

## [v0.12.0] - 2026-03-21

### Added
- add frontend performance monitoring to CI/CD pipeline

### Fixed
- upgrade rustls-webpki 0.103.9 → 0.103.10 (RUSTSEC-2026-0049)

## [v0.11.2] - 2026-03-20

### Fixed
- add "Add Device" button to /devices top-right corner

## [v0.11.1] - 2026-03-20

### Fixed
- UI bugs — duplicate buttons, search placeholder, device version sync

## [v0.11.0] - 2026-03-20

### Added
- complete agent auto-update system (Phase 14, ADR-005)

### Changed
- extract constants and helpers in Phase 14 code

## [v0.10.0] - 2026-03-20

### Added
- add frontend-audit skill

## [v0.9.5] - 2026-03-20

### Fixed
- UI bugs, terminal hotkeys, file error feedback, agent uninstall

## [v0.9.4] - 2026-03-20

### Fixed
- use server CA for QUIC load test mTLS handshake

## [v0.9.3] - 2026-03-20

### Fixed
- tune UDP buffers for QUIC load test (100+ agents)
- session permissions, agent deregistration, UI cleanup

## [v0.9.2] - 2026-03-19

### Fixed
- only increment enrollment token use count when CSR is signed

## [v0.9.1] - 2026-03-19

### Fixed
- resolve all 53 code scanning alerts across Go, TypeScript, Rust, JS

## [v0.9.0] - 2026-03-19

### Added
- close web UI API coverage gaps, fix 4 agent/session bugs

## [v0.8.1] - 2026-03-18

### Fixed
- use X-Forwarded-Proto for relay URL scheme behind reverse proxy

## [v0.8.0] - 2026-03-18

### Added
- list all devices without requiring group_id filter

## [v0.7.7] - 2026-03-18

### Fixed
- nullable device group_id to prevent FK violation on agent registration

## [v0.7.6] - 2026-03-18

### Fixed
- CSR-based agent enrollment to resolve TLS error 48 (unknown_ca)

## [v0.7.5] - 2026-03-18

### Fixed
- use actual hostname as QUIC TLS SNI, fix E2E strict mode violation

### Changed
- DRY helpers, module splits, and polish across all layers

## [v0.7.4] - 2026-03-18

### Fixed
- add missing global-teardown.ts for Playwright config
- use cross for aarch64 musl build instead of musl.cc download

## [v0.7.3] - 2026-03-18

### Fixed
- E2E test improvements and public update manifests endpoint
- build agent with musl for static binaries (no glibc dependency)

## [v0.7.2] - 2026-03-18

### Fixed
- include QUIC hostname in server TLS certificate SANs

## [v0.7.1] - 2026-03-18

### Fixed
- resolve DNS hostnames in agent QUIC connection

## [v0.7.0] - 2026-03-17

### Added
- add OPENGATE_QUIC_HOST to override QUIC address in enrollment

## [v0.6.1] - 2026-03-17

### Fixed
- inject OPENGATE_GITHUB_REPO into install script

## [v0.6.0] - 2026-03-17

### Added
- add Playwright E2E tests to precommit checklist

## [v0.5.0] - 2026-03-17

### Added
- inject server URL into install script from request headers

## [v0.4.3] - 2026-03-17

### Fixed
- remove service worker precache that served stale index.html

## [v0.4.2] - 2026-03-17

### Fixed
- global-setup reads baseURL from Playwright config

## [v0.4.1] - 2026-03-17

### Fixed
- rename unused loop variable to _ to satisfy shellcheck SC2034
- reset staging DB before e2e tests for fresh admin bootstrap

## [v0.4.0] - 2026-03-17

### Added
- add security groups and permissions management

### Fixed
- use exact match for Management sidebar text in e2e test
- correct e2e selectors for admin heading and System badge
- block SPA rendering until auth hydration completes
- e2e admin page tests race condition and last-admin test
- e2e admin tests fail when not first user in shared DB

## [v0.3.0] - 2026-03-17

### Added
- add enrollment token management to setup page

## [v0.2.0] - 2026-03-17

### Added
- auto-sync agent manifests from GitHub Releases on startup

## [v0.1.1] - 2026-03-17

### Fixed
- switch tokio-tungstenite from native-tls to rustls

## [v0.1.0] - 2026-03-16

### Added
- add QUIC control loop, auto-tag CI job, and changelog
