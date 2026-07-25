import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useDeviceStore, type LogPaneSource } from './state/device-store';
import { fireAndForget } from '../../lib/fire-and-forget';
import { ChevronLeftIcon, ChevronRightIcon, RefreshIcon, SpinnerIcon } from '../../components/icons';

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

// Window pulled by the one automatic fetch a pane makes: the first time it is
// opened for a device that has no logs cached yet.
const FIRST_OPEN_WINDOW_SECONDS = 3600;

interface TimeWindow {
  from: string;
  to: string;
}

interface LogExplorerProps {
  readonly deviceId: string;
  /** Which pane this instance drives (independent per-source store state). */
  readonly source: LogPaneSource;
  /** Card heading. */
  readonly title: string;
  /** System logs only: show the auto-detected unit dropdown + `target` column. */
  readonly showUnitFilter?: boolean;
  /** Correlation jump: pre-filter the explorer to this window and fetch it. */
  readonly focusWindow?: TimeWindow | null;
  /** Start closed, so opening a device page costs nothing (System Logs opt-in). */
  readonly startCollapsed?: boolean;
  /** On the first open with an empty cache, pull the most-recent default window. */
  readonly loadOnFirstOpen?: boolean;
}

function formatWindow(w: TimeWindow): string {
  return `${new Date(w.from).toLocaleString()} – ${new Date(w.to).toLocaleString()}`;
}

/**
 * Shared raw-log explorer used by both the Agent Logs (`source=agent`) and
 * System Logs (`source=host`) panes: a severity dropdown + level facets, keyword
 * search, a time-window selector, and pagination. The System Logs instance adds
 * an auto-detected unit dropdown and a clickable `target` column. Each source
 * reads and writes its own slice of the store, so the two panes never clobber.
 */
export function LogExplorer({ deviceId, source, title, showUnitFilter = false, focusWindow = null, startCollapsed = false, loadOnFirstOpen = false }: LogExplorerProps) {
  // Explicit source selection (not `s.logs[source]`) so the security linter can
  // see the access is over a fixed, closed key set.
  const logs = useDeviceStore((s) => (source === 'agent' ? s.logs.agent : s.logs.system));
  const logsLoading = useDeviceStore((s) => (source === 'agent' ? s.logsLoading.agent : s.logsLoading.system));
  const fetchLogs = useDeviceStore((s) => s.fetchLogs);

  const [level, setLevel] = useState('');
  const [search, setSearch] = useState('');
  const [unit, setUnit] = useState('');
  const [offset, setOffset] = useState(0);
  const [timeWindow, setTimeWindow] = useState<TimeWindow | null>(null);
  const [collapsed, setCollapsed] = useState(startCollapsed);
  const containerRef = useRef<HTMLDivElement | null>(null);

  const runFetch = useCallback((nextOffset: number, lvl: string, win: TimeWindow | null, unitFilter: string) => {
    setOffset(nextOffset);
    fireAndForget(fetchLogs(source, deviceId, {
      level: lvl || undefined,
      search: search || undefined,
      unit: showUnitFilter ? (unitFilter || undefined) : undefined,
      from: win?.from,
      to: win?.to,
      offset: nextOffset,
      limit: LIMIT,
    }));
  }, [source, deviceId, fetchLogs, search, showUnitFilter]);

  const handlePrevPage = useCallback(() => { runFetch(Math.max(0, offset - LIMIT), level, timeWindow, unit); }, [runFetch, offset, level, timeWindow, unit]);
  const handleNextPage = useCallback(() => { runFetch(offset + LIMIT, level, timeWindow, unit); }, [runFetch, offset, level, timeWindow, unit]);
  const selectLevel = useCallback((lvl: string) => { setLevel(lvl); runFetch(0, lvl, timeWindow, unit); }, [runFetch, timeWindow, unit]);
  const selectUnit = useCallback((u: string) => { setUnit(u); runFetch(0, level, timeWindow, u); }, [runFetch, level, timeWindow]);

  const selectRange = useCallback((seconds: number) => {
    const to = new Date();
    const from = new Date(to.getTime() - seconds * 1000);
    const win = { from: from.toISOString(), to: to.toISOString() };
    setTimeWindow(win);
    runFetch(0, level, win, unit);
  }, [runFetch, level, unit]);

  const clearWindow = useCallback(() => { setTimeWindow(null); runFetch(0, level, null, unit); }, [runFetch, level, unit]);

  const refresh = useCallback(() => { runFetch(0, level, timeWindow, unit); }, [runFetch, level, timeWindow, unit]);

  // Correlation jump: apply an incoming focus window, fetch it, and scroll in.
  // The action is captured in a ref so the effect fires only on window change.
  const applyFocusRef = useRef<(w: TimeWindow) => void>(() => undefined);
  useEffect(() => {
    applyFocusRef.current = (w: TimeWindow) => {
      // A drilled-to window opens the pane: the operator asked for these lines.
      setCollapsed(false);
      setTimeWindow(w);
      runFetch(0, level, w, unit);
    };
  });
  useEffect(() => {
    if (!focusWindow) return;
    applyFocusRef.current(focusWindow);
    containerRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, [focusWindow]);

  // The single automatic pull an opted-in pane makes: the first time it is
  // opened with nothing cached for this device, fetch the recent default window
  // so `available_units` and entries are there. Everything after that is
  // manual — a range button, a filter, or Refresh. Re-opening a device page
  // renders the cached response and pulls nothing. A correlation focusWindow
  // drives its own fetch, so it wins and this is skipped.
  const hasLogs = logs !== null;
  const didFirstOpenLoadRef = useRef(false);
  useEffect(() => {
    if (!loadOnFirstOpen || collapsed || focusWindow || hasLogs || didFirstOpenLoadRef.current) return;
    didFirstOpenLoadRef.current = true;
    selectRange(FIRST_OPEN_WINDOW_SECONDS);
  }, [loadOnFirstOpen, collapsed, focusWindow, hasLogs, selectRange]);

  // Level facets over the returned page — a point-and-click quick filter.
  const facets = useMemo(() => {
    const counts = new Map<string, number>();
    for (const e of logs?.entries ?? []) counts.set(e.level, (counts.get(e.level) ?? 0) + 1);
    return [...counts.entries()].sort((a, b) => b[1] - a[1]);
  }, [logs]);

  const availableUnits = logs?.available_units ?? [];

  return (
    <div ref={containerRef}>
      <div className="flex items-center justify-between mb-2">
        <button
          type="button"
          onClick={() => setCollapsed((c) => !c)}
          aria-expanded={!collapsed}
          aria-label={collapsed ? `Expand ${title}` : `Collapse ${title}`}
          className="text-sm font-semibold text-gray-300 flex items-center gap-2"
        >
          <span className={`text-xs transition-transform ${collapsed ? '' : 'rotate-90'}`} aria-hidden="true">&#9654;</span>
          {title}
        </button>
        <div className="flex items-center gap-2">
          {logsLoading && <span className="text-xs text-gray-500">Fetching…</span>}
          {!collapsed && (
            <button
              type="button"
              onClick={refresh}
              disabled={logsLoading}
              aria-label={`Refresh ${title}`}
              title={`Refresh ${title}`}
              className="px-2.5 py-1.5 bg-blue-600 hover:bg-blue-500 rounded text-xs font-medium disabled:opacity-50 inline-flex items-center"
            >
              {logsLoading ? <SpinnerIcon /> : <RefreshIcon />}
            </button>
          )}
        </div>
      </div>

      {collapsed ? null : (
        <>
        <div className="flex gap-2 mb-2 flex-wrap">
          <select
            value={level}
            onChange={(e) => selectLevel(e.target.value)}
            aria-label="Severity"
            className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-xs"
          >
            {levels.map((l) => (
              <option key={l} value={l}>{l || 'All Levels'}</option>
            ))}
          </select>
          {showUnitFilter && (
            <select
              value={unit}
              onChange={(e) => selectUnit(e.target.value)}
              aria-label="Unit"
              className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-xs max-w-48"
            >
              <option value="">All units</option>
              {availableUnits.map((u) => (
                <option key={u} value={u}>{u}</option>
              ))}
            </select>
          )}
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') runFetch(0, level, timeWindow, unit); }}
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

        {logs && (logs.entries.length > 0 ? (
          <>
            <div className="resize-y overflow-auto min-h-24 max-h-160 h-96 bg-gray-900 border border-gray-700 rounded p-2">
              <table className="w-full font-mono text-xs">
                <tbody>
                  {logs.entries.map((entry, i) => (
                    <tr key={`${entry.timestamp}-${String(i)}`} className="hover:bg-gray-800">
                      <td className="pr-2 text-gray-500 whitespace-nowrap align-top">{entry.timestamp}</td>
                      <td className={`pr-2 font-semibold whitespace-nowrap align-top ${levelColors.get(entry.level) ?? 'text-gray-400'}`}>
                        {entry.level.padEnd(5)}
                      </td>
                      {showUnitFilter && (
                        <td className="pr-2 whitespace-nowrap align-top">
                          {entry.target ? (
                            <button
                              type="button"
                              onClick={() => selectUnit(entry.target)}
                              title={`Filter to ${entry.target}`}
                              className="text-cyan-400 hover:underline"
                            >
                              {entry.target}
                            </button>
                          ) : (
                            <span className="text-gray-600">—</span>
                          )}
                        </td>
                      )}
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
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  onClick={handlePrevPage}
                  disabled={offset === 0 || logsLoading}
                  aria-label="Previous page"
                  className="px-2 py-1 bg-yellow-600 hover:bg-yellow-700 text-white rounded disabled:opacity-50 inline-flex items-center"
                >
                  <ChevronLeftIcon />
                </button>
                <button
                  type="button"
                  onClick={handleNextPage}
                  disabled={!logs.has_more || logsLoading}
                  aria-label="Next page"
                  className="px-2 py-1 bg-yellow-600 hover:bg-yellow-700 text-white rounded disabled:opacity-50 inline-flex items-center"
                >
                  <ChevronRightIcon />
                </button>
              </div>
            </div>
          </>
        ) : (
          <p className="text-xs text-gray-500">No logs available</p>
        ))}
        </>
      )}
    </div>
  );
}
