# QUIC Stream Ownership Workaround — Revert Plan

## Status: DEFERRED — waiting for quic-go update

## Problem

With `quic-go` v0.48.2 (and confirmed on v0.59.0), when mTLS client certificates are
used, `AcceptStream` on the server blocks indefinitely until the client writes data on
the stream. This creates a deadlock with our protocol, which requires the server to
send `ServerHello` first:

```
Server: AcceptStream (blocks waiting for client data)
Client: OpenStream → Read ServerHello (blocks waiting for server write)
→ Deadlock: both sides waiting on each other
```

## Current Workaround (applied in Phase 4, commit 97cb935)

Reversed stream ownership — the **server** calls `OpenStreamSync` and the **client**
calls `AcceptStream` for the control stream.

### Files changed:
- `server/internal/agentapi/server.go` — `accept()` method uses `conn.OpenStreamSync(ctx)`
- `server/tests/integration/agentapi_test.go` — `connectAgent()` uses `conn.AcceptStream(ctx)`

### Impact on architecture (from design doc §3.3, §4.3):
- Fast-path reconnection (`[0x14]` cached cert hash) gains an extra round-trip
- QUIC stream IDs are odd (server-initiated) instead of even (client-initiated) for control
- At >20,000 agents, mass reconnection storms cause goroutine pressure from blocked
  `OpenStreamSync` calls — server becomes the bottleneck pushing data first
- Kubernetes QUIC gateway pod restarts amplify the problem (5,000+ simultaneous stream opens)

## Revert Plan (when quic-go fixes AcceptStream + client cert behavior)

### Step 1: Test the new quic-go version

Write a diagnostic test that reproduces the original deadlock pattern with the new version:

```go
func TestQUIC_AcceptStreamWithClientCert(t *testing.T) {
    // Setup: mTLS with RequireAndVerifyClientCert
    // Client: OpenStreamSync → read (do NOT write first)
    // Server: AcceptStream → write "hello"
    // If this passes without hanging, the fix is in
}
```

Run with `-timeout 5s` — if it passes, proceed. If it hangs, the issue persists.

### Step 2: Revert server.go

In `server/internal/agentapi/server.go`, `accept()` method:

```go
// REVERT FROM:
stream, err := conn.OpenStreamSync(ctx)

// REVERT TO:
stream, err := conn.AcceptStream(ctx)
```

### Step 3: Revert integration test

In `server/tests/integration/agentapi_test.go`, `connectAgent()`:

```go
// REVERT FROM:
stream, err := conn.AcceptStream(ctx)

// REVERT TO:
stream, err := conn.OpenStreamSync(ctx)
```

### Step 4: Update Rust agent (if implemented by then)

In `agent/crates/mesh-agent-core/src/connection.rs`, the real QUIC connection code
should use `open_bi()` instead of `accept_bi()` for the control stream. Currently
uses mock duplex streams so no change needed yet.

### Step 5: Run tests

```bash
cd server && go test -race -timeout 30s ./...
cd server && go test -race -timeout 30s ./tests/integration/ -run TestAgentConnect
```

All 3 integration tests must pass: RegistersDevice, HeartbeatUpdatesLastSeen, DisconnectSetsOffline.

### Step 6: Also add timeout (regardless of revert)

Add a context timeout around the stream open/accept in `accept()` to prevent goroutine
leaks under adversarial conditions:

```go
streamCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()
stream, err := conn.AcceptStream(streamCtx)
```

### Step 7: Update go.mod

```bash
cd server && go get github.com/quic-go/quic-go@vX.Y.Z && go mod tidy
```

Note: check for API changes between versions. v0.48 uses `quic.Connection` (interface)
and `quic.Stream` (interface). Later versions may change to concrete types (`*quic.Conn`,
`*quic.Stream`).

## How to check for a fix

- Watch https://github.com/quic-go/quic-go/issues for AcceptStream + client cert issues
- Test each new minor release with the diagnostic test from Step 1
- The quic-go changelog may not explicitly mention this — always test empirically
