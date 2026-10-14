// Public surface of this feature. Cross-feature consumers MUST import from here.
export { useSessionStore } from './state/session-store';
export { useConnectionStore } from './state/connection-store';
export { holderLine } from './holder';
export { focusSessionTab, holdsSessionTab, openSessionTab } from './session-tab';
