# Phase 11 Post-Commit Refactoring Plan

## Context
Phase 11 (Intel AMT MPS, CIRA/APF protocol, WSMAN client) was just committed. This refactoring improves readability, eliminates duplication, and fixes a CLAUDE.md convention violation — without changing business logic or API signatures.

---

## Step 1: Sentinel error for `ErrDeviceNotConnected`
**Problem**: `handlers_amt.go:51` uses `strings.Contains(err.Error(), "not connected")` — violates CLAUDE.md Go convention (`errors.Is`/`errors.As` only).

- **`server/internal/amt/service.go`**: Add `var ErrDeviceNotConnected = errors.New("device not connected")`. Use it in `PowerAction` (line 37) and `QueryDeviceInfo` (line 47) instead of `fmt.Errorf`.
- **`server/internal/api/handlers_amt.go`**: Replace `strings.Contains` with `errors.Is(err, amt.ErrDeviceNotConnected)`. Remove `"strings"` import, add `"errors"` + `amt` import.
- **`server/internal/api/helpers_test.go`**: `stubAMTOperator` methods return `amt.ErrDeviceNotConnected` instead of `fmt.Errorf`.
- **`server/internal/api/amt_handlers_test.go`**: Line 98 — use `assert.Equal(t, "device not connected", apiErr.Error)` instead of `assert.Contains`.

## Step 2: Extract `maxAPFStringLen` constant
**Problem**: Magic number `256` hardcoded 5 times in `apf.go`.

- **`server/internal/mps/apf.go`**: Add `const maxAPFStringLen = 256`. Replace all 5 occurrences (lines 462, 482, 502, 551, 583).

## Step 3: Remove duplicate `encodeString` test helper
**Problem**: `apf_test.go` defines `encodeString()` identical to `encodeAPFString()` in `mps.go` (same package).

- **`server/internal/mps/apf_test.go`**: Delete `encodeString` (lines 378-383). Replace all calls with `encodeAPFString` (~10 call sites).

## Step 4: Extract `recordBoundPort` helper + fix logging
**Problem**: tcpip-forward parsing duplicated in `hsPfwdService` (line 303) and `handleMessage` (line 345). Logging level inconsistent (Info vs Debug).

- **`server/internal/mps/mps.go`**: Add `recordBoundPort(mc *Conn, gr *GlobalRequest)` method. Use it in both locations. Standardize both to `Debug` level.

## Step 5: Replace custom `indexOf` with `strings.Index`
**Problem**: `operations.go:138-145` reimplements `strings.Index()` with naive O(n*m) search.

- **`server/internal/mps/wsman/operations.go`**: Delete `indexOf`. Replace 3 call sites with `strings.Index`. Add `"strings"` import.

## Step 6: Extract `amtWSManPort` constant
**Problem**: Magic number `16992` hardcoded in `client.go`.

- **`server/internal/mps/wsman/client.go`**: Add `const amtWSManPort = 16992`. Use in `OpenChannel` call (line 40) and `Host` header (line 79).

## Step 7: Remove redundant variable in `xml.go`
**Problem**: `bodyContent` variable is unnecessary — just use `body` directly.

- **`server/internal/mps/wsman/xml.go`**: Remove lines 17-18 (`var bodyContent`/`if` block). Use `body` in the Sprintf template.

## Step 8: Pre-allocate in `readUserAuthRequest`
**Problem**: 6 appends on nil slice in `apf.go:475`.

- **`server/internal/mps/apf.go`**: Change `var result []byte` to `result := make([]byte, 0, 3*(4+64))`.

## Step 9: Add missing tests
New tests (table-driven, `testify`, same package access):

- **`server/internal/mps/apf_test.go`**:
  - `TestReadStringMsgOversized` — service request with string > maxAPFStringLen
  - `TestReadUserAuthRequestOversized` — auth string > maxAPFStringLen
  - `TestParseChannelDataBadDataString` — valid channel ID but malformed data string

- **`server/internal/mps/wsman/client_test.go`**:
  - `TestChannelConnWriteErrorPropagation` — writeFn returns error, verify Write returns (0, err)

- **`server/internal/mps/wsman/operations_test.go`**:
  - `TestExtractXMLFieldWithNamespacePrefix` — test `p:`, `g:`, `h:` prefix extraction
  - `TestExtractXMLFieldMissing` — missing tag and missing close tag return `""`

---

## Verification
After each step: `cd /home/ivan/opengate/server && go build ./... && go test ./internal/mps/... ./internal/amt/... ./internal/api/...`

After all steps: run `/precommit`.
