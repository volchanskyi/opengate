import { LogExplorer } from './LogExplorer';

interface TimeWindow {
  from: string;
  to: string;
}

interface DeviceLogsProps {
  readonly deviceId: string;
  /** Correlation jump: pre-filter the explorer to this window and fetch it. */
  readonly focusWindow?: TimeWindow | null;
}

/** Agent Logs pane: the agent's own rotated `tracing` files (`source=self`). */
export function DeviceLogs({ deviceId, focusWindow = null }: DeviceLogsProps) {
  return <LogExplorer deviceId={deviceId} source="agent" title="Agent Logs" focusWindow={focusWindow} />;
}
