import { LogExplorer } from './LogExplorer';

interface TimeWindow {
  from: string;
  to: string;
}

interface SystemLogsProps {
  deviceId: string;
  /** Correlation jump: pre-filter the explorer to this window and fetch it. */
  focusWindow?: TimeWindow | null;
}

/**
 * System Logs pane: the platform host log (`source=host` → journald on Linux,
 * Windows Event Log on Windows), with an auto-detected unit dropdown and a
 * clickable `target` column.
 */
export function SystemLogs({ deviceId, focusWindow = null }: SystemLogsProps) {
  return <LogExplorer deviceId={deviceId} source="system" title="System Logs" showUnitFilter focusWindow={focusWindow} autoLoadOnMount />;
}
