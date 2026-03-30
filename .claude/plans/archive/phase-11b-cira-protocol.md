# Phase 11b: Full CIRA/APF Protocol

## Context

Phase 11 implemented the MPS skeleton: APF codec, TLS listener, 5-step handshake, basic message loop, and channel management. However several protocol features are incomplete or missing:
- Handshake only handles 1 tcpip-forward (real AMT sends 2-3)
- No keepalive messages (208-211) — connection health goes unmonitored
- Channel flow control ignored (WindowAdj read but not tracked)
- UUID extraction lacks Intel mixed-endian byte reordering
- No WSMAN tunneling — can't query or control devices
- No power control or device info queries
- No HTTP API for AMT operations

Phase 11b completes the CIRA protocol so the MPS can fully manage connected AMT devices.

---

## Stage 1: APF Keepalive Codec

**Files:** `server/internal/mps/apf.go`, `apf_test.go`

### apf.go changes
- Add constants: `APFKeepaliveRequest=208`, `APFKeepaliveReply=209`, `APFKeepaliveOptionsRequest=210`, `APFKeepaliveOptionsReply=211`
- Add cases in `readMessageBody` switch:
  - 208, 209 → `readFixed(r, 4)` (4-byte cookie)
  - 210, 211 → `readFixed(r, 8)` (interval + timeout)
- Add parse structs: `KeepaliveRequest{Cookie uint32}`, `KeepaliveOptions{Interval, Timeout uint32}`
- Add write functions: `WriteKeepaliveRequest(w, cookie)`, `WriteKeepaliveReply(w, cookie)`, `WriteKeepaliveOptionsRequest(w, interval, timeout)`

### Tests (write first)
- `TestWriteReadKeepaliveRequest_Roundtrip`
- `TestWriteReadKeepaliveReply_Roundtrip`
- `TestWriteReadKeepaliveOptions_Roundtrip`
- `TestReadMessage_KeepaliveTypes` — verify dispatch for 208-211

---

## Stage 2: Intel GUID Reordering + ParseForwardData

**Files:** `server/internal/mps/apf.go`, `apf_test.go`

### apf.go changes
- Add `ReorderIntelGUID(raw [16]byte) uuid.UUID` — mixed-endian reorder: `[3,2,1,0]-[5,4]-[7,6]-[8,9]-[10:16]`
- Add `ParseForwardData(data []byte) (addr string, port uint32, err error)` — parse address+port from GlobalRequest.Data

### Tests
- `TestReorderIntelGUID` — known AMT GUID vectors
- `TestReorderIntelGUID_AllZeros`
- `TestParseForwardData` — table-driven with valid/invalid inputs

---

## Stage 3: Multi-Port Binding + Bound Port Tracking

**Files:** `server/internal/mps/mps.go`, `mps_test.go`

### mps.go changes
- Add `BoundPort{Address string; Port uint32}` struct
- Add `BoundPorts []BoundPort` field to `Conn`
- In `hsExchangeVersion`: call `ReorderIntelGUID(pv.UUID)` for proper UUID
- In `hsPfwdService`: keep existing single GlobalRequest handling, but also track the port in `mc.BoundPorts`
- In `handleMessage` case `APFGlobalRequest`: when request is `"tcpip-forward"`, parse addr/port via `ParseForwardData(gr.Data)` and append to `mc.BoundPorts`
- This way: first port captured in handshake, subsequent ports captured in message loop (no handshake timeout issues)

### Tests
- `TestHandshake_TracksFirstBoundPort`
- `TestMessageLoop_TracksAdditionalPorts` — simulate AMT sending 3 tcpip-forward after handshake

---

## Stage 4: Channel Flow Control

**Files:** `server/internal/mps/mps.go`, `mps_test.go`

### mps.go changes
- Update `DefaultWindowSize` from `0x4000` (16K) to `0x8000` (32K) to match MeshCentral
- Add to `Channel` struct: `sendWindow int64`, `recvConsumed int64`
- In `handleChannelOpen`: init `ch.sendWindow = int64(co.InitialWindowSz)`
- In `handleChannelData`: increment `ch.recvConsumed += len(cd.Data)`; when `recvConsumed >= DefaultWindowSize/2`, send `WriteChannelWindowAdj` and reset
- In `handleMessage` case `APFChannelWindowAdj`: parse recipient+bytesToAdd, find channel, add to `sendWindow`
- In `OpenChannel`: after confirmation, parse window size from response to init `sendWindow`
- Add `Channel.SendData(w io.Writer, data []byte) error` that checks/decrements `sendWindow` before calling `WriteChannelData`

### Tests
- `TestChannelData_SendsWindowAdjust` — verify WindowAdj sent after receiving enough data
- `TestChannelWindowAdj_IncrementsSendWindow`

---

## Stage 5: Server-Initiated Keepalive

**Files:** `server/internal/mps/mps.go`, `mps_test.go`

### mps.go changes
- Add to `Conn`: `lastActivity atomic.Int64` (updated on every received message)
- In `handleMessage`: add cases for `APFKeepaliveRequest` (reply with cookie), `APFKeepaliveReply` (update lastActivity), `APFKeepaliveOptionsReply` (log only)
- Add `startKeepalive(ctx, mc)` goroutine launched after handshake:
  1. Send `WriteKeepaliveOptionsRequest(mc.netConn, 30, 10)` (30s interval, 10s timeout)
  2. Ticker every 30s: send `WriteKeepaliveRequest` with incrementing cookie
  3. Stop on ctx.Done()
- In `handleConn`: derive per-connection context `connCtx, connCancel := context.WithCancel(ctx)`; `defer connCancel()`; launch `go s.startKeepalive(connCtx, mc)` before `messageLoop`
- The existing 90s read deadline in `messageLoop` acts as dead-connection detector

### Tests
- `TestKeepalive_RequestReplyEcho` — device receives keepalive, verify reply sent
- `TestKeepalive_OptionsNegotiation` — verify server sends KeepaliveOptionsRequest after handshake

---

## Stage 6: WSMAN Tunneling + Digest Auth

**New package:** `server/internal/mps/wsman/`

### wsman/digest.go
- `DigestAuth{Username, Password string}`
- `Authorize(method, uri, wwwAuth string) (string, error)` — parse WWW-Authenticate, compute RFC 2617 response, return Authorization header
- Internal: `parseChallenge(header) map[string]string`, `md5Hash(...)`, `computeResponse(...)`

### wsman/channel_conn.go
- `ChannelConn` adapts MPS channel → `io.ReadWriteCloser`
- Uses `io.Pipe` for read side: message loop calls `Feed(data)` → writes to pipe writer
- Write side: calls `mps.WriteChannelData` on the connection
- Add `OnData func([]byte)` callback field to `mps.Channel` (small change to mps.go)
- In `mps.handleChannelData`: if `ch.OnData != nil`, call it instead of writing to `ch.fwd`

### wsman/client.go
- `Client{conn *mps.Conn, auth DigestAuth, mu sync.Mutex, logger}`
- `NewClient(conn, username, password, logger) *Client`
- `Do(ctx, soapAction, body []byte) ([]byte, error)`:
  1. Lock mu (one WSMAN request at a time per connection)
  2. Open channel to 127.0.0.1:16992 via `conn.OpenChannel`
  3. Create `ChannelConn` from channel, register `OnData` callback
  4. Send HTTP POST `/wsman` (no auth header)
  5. Read HTTP response; if 401, parse WWW-Authenticate, retry with Digest auth
  6. Return response body
  7. Close channel

### wsman/xml.go
- `Envelope(resourceURI, action, selectorSet, body string) []byte` — SOAP envelope with WS-Addressing headers
- `PowerStateChangeBody(state int) string` — CIM_PowerManagementService.RequestPowerStateChange
- `GetBody() string` — empty body for Get operations
- `EnumerateBody() string`, `PullBody(enumCtx string) string`
- `ParseEnvelopeBody(data []byte) ([]byte, error)` — extract SOAP body from response
- Use `encoding/xml` for parsing, `fmt.Sprintf` for templates (fixed structure, no need for text/template)

### Tests
- `wsman/digest_test.go`: `TestParseChallenge`, `TestComputeResponse_RFC2617Vectors`, `TestAuthorize`
- `wsman/client_test.go`: `TestDo_Success`, `TestDo_DigestAuth401Retry`, `TestChannelConn_ReadWrite`
- `wsman/xml_test.go`: `TestEnvelope_Structure`, `TestPowerStateChangeBody`, `TestParseEnvelopeBody`

---

## Stage 7: Power Control + Device Info Operations

**Files:** `server/internal/mps/wsman/operations.go`, `operations_test.go`

### operations.go
- `PowerState int` type with constants: `PowerOn=2`, `PowerCycle=5`, `SoftOff=8`, `HardReset=10`
- `DeviceInfo{Hostname, Model, Firmware string}`
- `Client.RequestPowerStateChange(ctx, state PowerState) error`
- `Client.GetDeviceInfo(ctx) (*DeviceInfo, error)` — queries CIM_ComputerSystem + AMT_SetupAndConfigurationService
- `Client.GetPowerState(ctx) (PowerState, error)`

### mps.go changes
- Add `AMTCredentials{Username, Password string}` struct
- Update `NewServer(cm, store, creds AMTCredentials, logger)` signature
- Add `Server.PowerAction(ctx, amtUUID, state) error` — gets conn, creates wsman.Client, calls RequestPowerStateChange
- Add `Server.QueryDeviceInfo(ctx, amtUUID) (*wsman.DeviceInfo, error)` — queries device, updates DB with learned info
- In `registerConn`: optionally launch `go s.QueryDeviceInfo(ctx, amtUUID)` (best-effort, log errors)

### main.go changes
- Add `--amt-user` and `--amt-pass` flags (default: "admin", empty)
- Pass `mps.AMTCredentials{*amtUser, *amtPass}` to `mps.NewServer`

### Tests
- `TestPowerAction_DeviceNotConnected`
- `TestQueryDeviceInfo_UpdatesDB`

---

## Stage 8: HTTP API for AMT Operations

**Files:** `api/openapi.yaml`, `server/internal/api/api.go`, `server/internal/api/handlers_amt.go` (new), `server/internal/api/amt_handlers_test.go` (new)

### openapi.yaml additions
- Schema `AMTDevice`: uuid, hostname, model, firmware, status, last_seen, bound_ports
- Schema `AMTPowerRequest`: action (enum: power_on, power_cycle, soft_off, hard_reset)
- `GET /api/v1/amt/devices` — list AMT devices (admin, bearerAuth)
- `GET /api/v1/amt/devices/{uuid}` — get AMT device
- `POST /api/v1/amt/devices/{uuid}/power` — send power command (409 if not connected)
- `GET /api/v1/amt/devices/{uuid}/info` — refresh device info via WSMAN (409 if not connected)

### api.go changes
- Add `AMTOperator` interface:
  ```go
  type AMTOperator interface {
      PowerAction(ctx context.Context, amtUUID uuid.UUID, state int) error
      QueryDeviceInfo(ctx context.Context, amtUUID uuid.UUID) (*wsman.DeviceInfo, error)
      ConnectedDeviceCount() int
  }
  ```
- Add `amt AMTOperator` field to `Server` struct
- Update `NewServer` signature to accept `amt AMTOperator`
- Regenerate: `go generate ./internal/api/...`

### handlers_amt.go
- Implement generated strict server interface methods for the 4 endpoints
- Use `srv.amt` for operations, `srv.store` for DB reads

### helpers_test.go changes
- Add `stubAMTOperator` test double
- Update `newTestServer` and `newTestServerWithAgents` to pass `&stubAMTOperator{}`

### main.go changes
- Pass `mpsSrv` as `AMTOperator` to `api.NewServer`

### Tests
- `TestListAMTDevices_Empty`, `TestListAMTDevices_WithDevices`
- `TestGetAMTDevice_NotFound`, `TestGetAMTDevice_Found`
- `TestAmtPowerAction_NotConnected` (409)
- `TestGetAMTDeviceInfo_NotConnected` (409)

---

## Key Design Decisions

1. **WSMAN as sub-package** (`server/internal/mps/wsman/`): separates HTTP/SOAP/XML concerns from APF transport
2. **ChannelConn with io.Pipe**: bridges event-driven message loop with synchronous HTTP request/response
3. **OnData callback on Channel**: minimal change to mps.go to support data routing to ChannelConn
4. **Per-connection mutex for WSMAN**: one request at a time avoids channel data interleaving
5. **DefaultWindowSize → 32K**: matches MeshCentral, reduces WindowAdj frequency
6. **AMTOperator interface**: decouples API handlers from MPS internals (testable)
7. **Multi-port handled in message loop**: no handshake timeout issues; first port in handshake, rest in loop

## Modified Files Summary

| File | Change |
|------|--------|
| `server/internal/mps/apf.go` | Keepalive constants/read/write, GUID reorder, ParseForwardData |
| `server/internal/mps/apf_test.go` | Keepalive + GUID + forward data tests |
| `server/internal/mps/mps.go` | BoundPorts, flow control, keepalive goroutine, OnData callback, AMTCredentials, PowerAction, QueryDeviceInfo |
| `server/internal/mps/mps_test.go` | Multi-port, keepalive, flow control tests |
| `server/internal/mps/wsman/digest.go` | **NEW** — Digest auth (RFC 2617) |
| `server/internal/mps/wsman/digest_test.go` | **NEW** |
| `server/internal/mps/wsman/channel_conn.go` | **NEW** — APF channel → io.ReadWriteCloser |
| `server/internal/mps/wsman/client.go` | **NEW** — WSMAN HTTP client over APF |
| `server/internal/mps/wsman/client_test.go` | **NEW** |
| `server/internal/mps/wsman/xml.go` | **NEW** — SOAP envelope templates, XML parsing |
| `server/internal/mps/wsman/xml_test.go` | **NEW** |
| `server/internal/mps/wsman/operations.go` | **NEW** — PowerAction, GetDeviceInfo, GetPowerState |
| `server/internal/mps/wsman/operations_test.go` | **NEW** |
| `api/openapi.yaml` | AMTDevice schema, 4 new endpoints |
| `server/internal/api/openapi_gen.go` | Regenerated |
| `server/internal/api/api.go` | AMTOperator interface, updated NewServer |
| `server/internal/api/handlers_amt.go` | **NEW** — AMT REST handlers |
| `server/internal/api/amt_handlers_test.go` | **NEW** |
| `server/internal/api/helpers_test.go` | stubAMTOperator, updated newTestServer |
| `server/cmd/meshserver/main.go` | --amt-user/--amt-pass flags, wire AMTOperator |

## Verification

1. `make test` — all unit tests pass (30+ new tests across stages)
2. `make lint` — go vet + golangci-lint clean
3. Manual test with real AMT hardware:
   - AMT device connects to MPS on :4433
   - Verify keepalive messages in logs
   - `GET /api/v1/amt/devices` shows device with hostname/firmware
   - `POST /api/v1/amt/devices/{uuid}/power` with `{"action":"hard_reset"}` resets device
4. `make build` — compiles cleanly
