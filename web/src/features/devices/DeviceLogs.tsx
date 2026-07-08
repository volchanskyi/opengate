import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useDeviceStore } from './state/device-store';
import { LogRateSparkline } from './LogRateSparkline';
import { fireAndForget } from '../../lib/fire-and-forget';

const levelColors = new Map<string, string>([
  ['ERROR', 'text-red-400'],
  ['WARN', 'text-yellow-400'],
  ['INFO', 'text-blue-400'],
  ['DEBUG', 'text-gray-400'],
  ['TRACE', 'text-gray-500'],
]);

const levels = ['', 'TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR'];

const RANGES = [
  { key: '15m', seconds: 15 * 60 },
  { key: '1h', seconds: 3600 },
  { key: '6h', seconds: 6 * 3600 },
  { key: '24h', seconds: 24 * 3600 },
];

const LIMIT = 300;

interface TimeWindow {
  from: string;
  to: string;
}

interface DeviceLogsProps {
  deviceId: string;
  /** Correlation jump: pre-filter the explorer to this window and fetch it. */
  focusWindow?: TimeWindow | null;
}

function formatWindow(w: TimeWindow): string {
  return `${new Date(w.from).toLocaleString()} – ${new Date(w.to).toLocaleString()}`;
}

export function DeviceLogs({ deviceId, focusWindow = null }: DeviceLogsProps) {
  const logs = useDeviceStore((s) => s.logs);
  const logsLoading = useDeviceStore((s) => s.logsLoading);
  const fetchLogs = useDeviceStore((s) => s.fetchLogs);
  const metrics = useDeviceStore((s) => s.metrics);

  const [level, setLevel] = useState('');
  const [search, setSearch] = useState('');
  const [offset, setOffset] = useState(0);
  const [timeWindow, setTimeWindow] = useState<TimeWindow | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);

  const runFetch = useCallback((nextOffset: number, lvl: string, win: TimeWindow | null) => {
    setOffset(nextOffset);
    fireAndForget(fetchLogs(deviceId, {
      level: lvl || undefined,
      search: search || undefined,
      from: win?.from,
      to: win?.to,
      offset: nextOffset,
      limit: LIMIT,
    }));
  }, [deviceId, fetchLogs, search]);

  const handleFetch = useCallback(() => { runFetch(0, level, timeWindow); }, [runFetch, level, timeWindow]);
  const handleLoadMore = useCallback(() => { runFetch(offset + LIMIT, level, timeWindow); }, [runFetch, offset, level, timeWindow]);
  const selectLevel = useCallback((lvl: string) => { setLevel(lvl); runFetch(0, lvl, timeWindow); }, [runFetch, timeWindow]);

  const selectRange = useCallback((seconds: number) => {
    const to = new Date();
    const from = new Date(to.getTime() - seconds * 1000);
    const win = { from: from.toISOString(), to: to.toISOString() };
    setTimeWindow(win);
    runFetch(0, level, win);
  }, [runFetch, level]);

  const clearWindow = useCallback(() => { setTimeWindow(null); runFetch(0, level, null); }, [runFetch, level]);

  // Correlation jump: apply an incoming focus window, fetch it, and scroll in.
  // The action is captured in a ref so the effect fires only on window change.
  const applyFocusRef = useRef<(w: TimeWindow) => void>(() => undefined);
  useEffect(() => {
    applyFocusRef.current = (w: TimeWindow) => { setTimeWindow(w); runFetch(0, level, w); };
  });
  useEffect(() => {
    if (!focusWindow) return;
    applyFocusRef.current(focusWindow);
    containerRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, [focusWindow]);

  // Level facets over the returned page — a point-and-click quick filter.
  const facets = useMemo(() => {
    const counts = new Map<string, number>();
    for (const e of logs?.entries ?? []) counts.set(e.level, (counts.get(e.level) ?? 0) + 1);
    return [...counts.entries()].sort((a, b) => b[1] - a[1]);
  }, [logs]);

  return (
    <div ref={containerRef}>
      <div className="flex items-center justify-between mb-2">
        <h3 className="text-sm font-semibold text-gray-300">Agent Logs</h3>
        <button
          type="button"
          onClick={handleFetch}
          disabled={logsLoading}
          className="px-3 py-1 bg-blue-600 hover:bg-blue-500 rounded text-xs font-medium disabled:opacity-50"
        >
          {logsLoading ? 'Fetching...' : 'Fetch Logs'}
        </button>
      </div>

      <LogRateSparkline metrics={metrics} />

      <div className="flex gap-2 mb-2">
        <select
          value={level}
          onChange={(e) => setLevel(e.target.value)}
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-xs"
        >
          {levels.map((l) => (
            <option key={l} value={l}>{l || 'All Levels'}</option>
          ))}
        </select>
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search keyword..."
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-xs flex-1"
        />
      </div>

      <div className="flex items-center gap-1 mb-2 flex-wrap">
        <span className="text-[10px] text-gray-500 mr-1">Window:</span>
        {RANGES.map((r) => (
          <button
            key={r.key}
            type="button"
            onClick={() => selectRange(r.seconds)}
            className="px-2 py-0.5 rounded text-[11px] bg-gray-700 text-gray-300 hover:bg-gray-600"
          >
            {r.key}
          </button>
        ))}
        {timeWindow && (
          <button
            type="button"
            onClick={clearWindow}
            className="px-2 py-0.5 rounded text-[11px] bg-blue-900/60 text-blue-200 hover:bg-blue-900"
            title={formatWindow(timeWindow)}
          >
            {formatWindow(timeWindow)} ✕
          </button>
        )}
      </div>

      {facets.length > 0 && (
        <div className="flex items-center gap-1 mb-2 flex-wrap">
          {facets.map(([lvl, count]) => (
            <button
              key={lvl}
              type="button"
              onClick={() => selectLevel(level === lvl ? '' : lvl)}
              className={`px-2 py-0.5 rounded text-[11px] ${level === lvl ? 'bg-blue-600 text-white' : 'bg-gray-700 hover:bg-gray-600'} ${levelColors.get(lvl) ?? ''}`}
            >
              {lvl} {count}
            </button>
          ))}
        </div>
      )}

      {logs && logs.entries.length > 0 ? (
        <>
          <div className="max-h-96 overflow-y-auto bg-gray-900 border border-gray-700 rounded p-2">
            <table className="w-full font-mono text-xs">
              <tbody>
                {logs.entries.map((entry, i) => (
                  <tr key={`${entry.timestamp}-${String(i)}`} className="hover:bg-gray-800">
                    <td className="pr-2 text-gray-500 whitespace-nowrap align-top">{entry.timestamp}</td>
                    <td className={`pr-2 font-semibold whitespace-nowrap align-top ${levelColors.get(entry.level) ?? 'text-gray-400'}`}>
                      {entry.level.padEnd(5)}
                    </td>
                    <td className="text-gray-300 whitespace-pre-wrap break-all">{entry.message}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex items-center justify-between mt-2 text-xs text-gray-400">
            <span>
              Showing {offset + 1}-{Math.min(offset + logs.entries.length, logs.total)} of {logs.total}
            </span>
            {logs.has_more && (
              <button
                type="button"
                onClick={handleLoadMore}
                disabled={logsLoading}
                className="px-2 py-1 bg-gray-700 hover:bg-gray-600 rounded disabled:opacity-50"
              >
                Load More
              </button>
            )}
          </div>
        </>
      ) : logs && logs.entries.length === 0 ? (
        <p className="text-xs text-gray-500">No logs available</p>
      ) : null}
    </div>
  );
}
