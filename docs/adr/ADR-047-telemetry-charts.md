---
number: 47
title: Telemetry charts run on a uPlot adapter
---

# ADR-047 — Telemetry charts run on a uPlot adapter

## Context

Device screens draw many series over long windows. A general charting library
pulled into the main bundle would slow every page, including the ones with no
chart on them.

## Decision

**One thin adapter over uPlot, on typed arrays, and it is the only module that
imports uPlot.** Everything else talks to the adapter, so the library can be
replaced without touching a feature.

**Split into its own bundle chunk** with its own size budget, so a page with no
chart does not download it.

**Bands say where they came from.** Central keeps averages only, so a chart
drawn from central data is labelled as averages rather than implying it is
showing peaks it does not have.

## Consequences

The logs explorer is built on the same adapter, and a jump from a chart to the
logs around that moment carries the time window with it.
