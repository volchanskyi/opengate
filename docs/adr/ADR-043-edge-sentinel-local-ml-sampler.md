---
adr: 043
title: Edge Sentinel Local ML Sampler
status: Accepted
date: 2026-07-01
---

# ADR-043: Edge Sentinel Local ML Sampler

## Status

Accepted.

## Context

Edge Sentinel needs agent-side anomaly hints without moving raw high-cardinality
host/process detail into the control plane. The design target is clean-room,
pure Rust, bounded in memory and CPU, and safe around command-line secrets.

## Decision

Add a default-off agent sampler and ML kernel:

- `mesh-agent-core::ml` owns a deterministic k=2 k-means model,
  all-model-consensus ensemble, rolling anomaly-bit window, sampler trait,
  `SysinfoSampler`, `FakeSampler`, and command-line redaction helpers.
- Process samples are bounded top-N ranks with executable basename and optional
  command-line hash. Full command lines are not collected by default.
- Secret redaction covers common assignment and flag-value forms, bearer tokens,
  AWS access keys, and credential-bearing URLs for future elevated/on-demand
  paths.
- `mesh-agent` starts the sampler on every agent; failures are logged and do not
  block normal control/session traffic.
- Always-run tests assert the hot detection loop is allocation-free after model
  load and that ensemble/window RSS delta stays under the local bound.
- A Criterion bench harness records detection latency, `sysinfo` sampling cost,
  and RSS probe evidence for ARM runner artifacts.

## Consequences

- The foundation can be tested without protocol or VictoriaMetrics ingestion.
- Later telemetry work can reuse the sampler and anomaly window.
- ARM64 runner artifact capture (footprint evidence on real ARM hardware) is an
  open follow-up tracked in [`techdebt.md`](../../.claude/techdebt.md).
