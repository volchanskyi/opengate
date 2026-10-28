---
number: 8
title: Cross-compiling the agent to aarch64 musl
---

# ADR-008 — Cross-compiling the agent to aarch64 musl

## Context

The deployment target is ARM64 and the CI runners are x86-64.

## Decision

The Rust agent cross-compiles to `aarch64-unknown-linux-musl` with `cross`; Go
builds for ARM64 natively. Both produce static binaries.

## Consequences

Docker is required in CI. SELinux hosts need the replaced binary relabelled
after an over-the-air update, or it will not execute.
