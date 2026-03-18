# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
