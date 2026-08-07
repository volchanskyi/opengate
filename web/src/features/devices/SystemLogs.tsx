import { LogExplorer } from './LogExplorer';

interface TimeWindow {
  from: string;
  to: string;
}

interface SystemLogsProps {
  readonly deviceId: string;
  /** Correlation jump: pre-filter the explorer to this window and fetch it. */
  readonly focusWindow?: TimeWindow | null;
}

/**
 * System Logs pane: the platform host log (`source=host` → journald on Linux),
 * with an auto-detected unit dropdown and a clickable `target` column.
 *
 * The pane starts closed and pulls once, on its first open per device: a host
 * log pull is a live round trip to the agent, so it happens when an operator
 * asks for it rather than on every visit to a device page.
 */
export function SystemLogs({ deviceId, focusWindow = null }: SystemLogsProps) {
  return (
    <LogExplorer
      deviceId={deviceId}
      source="system"
      title="System Logs"
      showUnitFilter
      focusWindow={focusWindow}
      startCollapsed
      loadOnFirstOpen
    />
  );
}
