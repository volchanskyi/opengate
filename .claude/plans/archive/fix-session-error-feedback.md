# Fix: Show Error Feedback When Start Session Fails

## Context

When an agent update fails (or an agent is restarting after a successful update), clicking "Start Session" silently does nothing — `createSession` returns `null` and the UI gives zero feedback. This makes the button appear broken/blocked, and when two devices are affected simultaneously it looks like a global issue.

## Root Cause

[DeviceDetail.tsx:100-105](web/src/features/devices/DeviceDetail.tsx#L100-L105):
```tsx
const handleStartSession = async () => {
  const result = await createSession(device.id);
  if (result) {
    navigate(`/sessions/${result.token}`, { ... });
  }
  // ← No else — silent failure
};
```

The `createSession` call in [session-store.ts:31-47](web/src/state/session-store.ts#L31-L47) uses `apiAction` which sets `store.error` on failure, but `DeviceDetail` never reads `session-store.error`.

## Plan

### Step 1: Surface error from `createSession`

Modify `handleStartSession` in [DeviceDetail.tsx](web/src/features/devices/DeviceDetail.tsx) to show a toast on failure:

```tsx
const handleStartSession = async () => {
  const result = await createSession(device.id);
  if (result) {
    navigate(`/sessions/${result.token}`, { state: { relayUrl: result.relay_url, capabilities: device.capabilities } });
  } else {
    addToast('Failed to start session — agent may be offline or restarting', 'error');
  }
};
```

This uses the existing `addToast` already imported at line 25.

### Step 2: Tests

Add/update Vitest tests for `DeviceDetail` to verify:
- Toast is shown when `createSession` returns `null`
- Navigation happens when `createSession` returns a valid result

**Test file**: [web/src/features/devices/DeviceDetail.test.tsx](web/src/features/devices/DeviceDetail.test.tsx) (create if needed, or add to existing)

## Files to Modify
- [web/src/features/devices/DeviceDetail.tsx](web/src/features/devices/DeviceDetail.tsx) — add else branch with toast

## Verification
1. `make test` — all existing tests pass
2. Manual: push update to agent, click Start Session during restart → see error toast
3. Manual: click Start Session on online agent → navigates normally
