# Phase 8 Post-Commit Refactoring Plan

## Context
Phase 8 added ~3100 lines across 49 files: web client session features, protocol codec, WebSocket transport, and state stores. Three independent explorations identified performance hotspots, duplications, and defensive coding gaps. No business logic or API signature changes.

---

## Unit 1: Fix `toBlob()` unnecessary buffer copy
**File:** `web/src/features/file-manager/file-transfer.ts:30`

- Replace `new Blob(this.chunks.map((c) => c.slice().buffer as ArrayBuffer))` with `new Blob(this.chunks)`
- `Blob` constructor natively accepts `Uint8Array[]`; `.slice().buffer` copies every chunk for no reason

**Verify:** `npx vitest run src/features/file-manager/file-transfer.test.ts`

---

## Unit 2: Hoist TextEncoder/TextDecoder in use-terminal.ts
**File:** `web/src/features/terminal/use-terminal.ts:31,42`

- Add module-level constants before the hook:
  ```typescript
  const textEncoder = new TextEncoder();
  const textDecoder = new TextDecoder();
  ```
- Line 31: replace `const encoder = new TextEncoder(); transport.sendTerminalData(encoder.encode(data))` with `transport.sendTerminalData(textEncoder.encode(data))`
- Lines 42-43: replace `const decoder = new TextDecoder(); term.write(decoder.decode(frame.data))` with `term.write(textDecoder.decode(frame.data))`

**Verify:** `npx vitest run src/features/terminal/TerminalView.test.tsx`

---

## Unit 3: Add default cases to codec.ts switches
**File:** `web/src/lib/protocol/codec.ts:19-32,86-98`

- `encodeFrame` switch (line 32): add `default: throw new Error('unexpected frame type')`
- `decodeFrame` switch (line 98): add `default: throw new Error('unexpected frame type')`
- TypeScript exhaustiveness already covers these, but explicit defaults are standard defensive practice

**Verify:** `npx vitest run src/lib/protocol/codec.test.ts`

---

## Unit 4: Deduplicate ChatMessage type
**Files:** `web/src/lib/protocol/types.ts`, `web/src/state/chat-store.ts`

- In `types.ts` after FileFrame interface (~line 45), export:
  ```typescript
  export interface ChatMessageFields { text: string; sender: string }
  ```
- Update ControlMessage union line 110: `| ({ type: 'ChatMessage' } & ChatMessageFields)`
- In `chat-store.ts`: remove local `ChatMessage` interface (lines 3-6), import:
  ```typescript
  import type { ChatMessageFields } from '../lib/protocol/types';
  type ChatMessage = ChatMessageFields;
  ```

**Verify:** `npx vitest run src/state/chat-store.test.ts && npx vitest run src/features/messenger/MessengerView.test.tsx`

---

## Unit 5: Unify SessionToolbar state config + parameterize tests
**Files:** `web/src/features/session/SessionToolbar.tsx`, `web/src/features/session/SessionToolbar.test.tsx`

- Replace two parallel `Record<ConnectionState, string>` maps (lines 8-20) with single:
  ```typescript
  const STATE_CONFIG: Record<ConnectionState, { label: string; color: string }> = {
    disconnected: { label: 'Disconnected', color: 'bg-gray-500' },
    connecting:   { label: 'Connecting...', color: 'bg-yellow-500' },
    connected:    { label: 'Connected', color: 'bg-green-500' },
    error:        { label: 'Error', color: 'bg-red-500' },
  };
  ```
- Update JSX to use `STATE_CONFIG[connectionState].label` and `.color`
- In test file: replace 4 repetitive test blocks with `it.each`

**Verify:** `npx vitest run src/features/session/SessionToolbar.test.tsx`

---

## Unit 6: Cache getBoundingClientRect in InputHandler
**File:** `web/src/features/remote-desktop/input-handler.ts`

- Add `private cachedRect: DOMRect | null = null` field
- Add `private boundWindowHandlers: Array<[string, EventListener]> = []` field
- In constructor after event setup: add `resize`/`scroll` listeners on `window` that set `this.cachedRect = null`
- In `scaleCoords()`: use cached rect, populate on first access:
  ```typescript
  if (!this.cachedRect) this.cachedRect = this.canvas.getBoundingClientRect();
  const rect = this.cachedRect;
  ```
- In `destroy()`: also remove window listeners

**Verify:** `npx vitest run src/features/remote-desktop/input-handler.test.ts`

---

## Issues Investigated and Excluded

| Issue | Reason for exclusion |
|---|---|
| API mock duplication across 7 test files | Mocks are NOT identical (5+ distinct patterns); shared helper would need complex parameterization |
| VALID_KEYS derived from KeyCode type | Requires runtime array export in pure-type file; affects tree-shaking |
| ws-transport event handler cleanup on reconnect | Old WebSocket GCs naturally; spec guarantees no events after close |
| Array index as React key in MessengerView | Messages are append-only; index keys are correct for this pattern |

---

## Verification
After all units, run full suite: `cd web && npx vitest run` then `make lint`
