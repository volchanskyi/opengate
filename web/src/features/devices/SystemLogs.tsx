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
 * SystemLogs shows the platform host log (journald on Linux). It starts closed because a pull
 * is a live round trip to the agent, and pulls once on the first open per device.
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
