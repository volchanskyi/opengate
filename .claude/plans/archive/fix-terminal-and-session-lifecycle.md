# Fix Terminal Rendering and Session Lifecycle

## Scope

- Load xterm.js's required stylesheet so its helper textarea remains hidden.
- Delete ended relay sessions through an explicit background, cross-organization repository path.
- Bound the wait for a missing relay peer so a browser-only or agent-only token cannot remain live forever.

## Validation

1. Add failing web and Go regression tests before source changes.
2. Run the focused terminal, session repository, relay HTTP, and composition-root tests.
3. Run web type/lint checks and the broader affected Go packages.
4. Reconcile ADR-059 and the implementation-phase record, then archive this plan.
