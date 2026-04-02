import { useState, useCallback } from 'react';
import { useDeviceStore } from '../../state/device-store';

const levelColors: Record<string, string> = {
  ERROR: 'text-red-400',
  WARN: 'text-yellow-400',
  INFO: 'text-blue-400',
  DEBUG: 'text-gray-400',
  TRACE: 'text-gray-500',
};

const levels = ['', 'TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR'];

interface DeviceLogsProps {
  deviceId: string;
}

export function DeviceLogs({ deviceId }: DeviceLogsProps) {
  const logs = useDeviceStore((s) => s.logs);
  const logsLoading = useDeviceStore((s) => s.logsLoading);
  const fetchLogs = useDeviceStore((s) => s.fetchLogs);

  const [level, setLevel] = useState('');
  const [search, setSearch] = useState('');
  const [offset, setOffset] = useState(0);
  const limit = 100;

  const handleFetch = useCallback(() => {
    setOffset(0);
    fetchLogs(deviceId, {
      level: level || undefined,
      search: search || undefined,
      offset: 0,
      limit,
    });
  }, [deviceId, fetchLogs, level, search]);

  const handleLoadMore = useCallback(() => {
    const newOffset = offset + limit;
    setOffset(newOffset);
    fetchLogs(deviceId, {
      level: level || undefined,
      search: search || undefined,
      offset: newOffset,
      limit,
    });
  }, [deviceId, fetchLogs, level, search, offset]);

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <h3 className="text-sm font-semibold text-gray-300">Logs</h3>
        <button
          type="button"
          onClick={handleFetch}
          disabled={logsLoading}
          className="px-3 py-1 bg-blue-600 hover:bg-blue-500 rounded text-xs font-medium disabled:opacity-50"
        >
          {logsLoading ? 'Fetching...' : 'Fetch Logs'}
        </button>
      </div>

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

      {logs && logs.entries.length > 0 ? (
        <>
          <div className="max-h-96 overflow-y-auto bg-gray-900 border border-gray-700 rounded p-2">
            <table className="w-full font-mono text-xs">
              <tbody>
                {logs.entries.map((entry, i) => (
                  <tr key={`${entry.timestamp}-${i}`} className="hover:bg-gray-800">
                    <td className="pr-2 text-gray-500 whitespace-nowrap align-top">{entry.timestamp}</td>
                    <td className={`pr-2 font-semibold whitespace-nowrap align-top ${levelColors[entry.level] ?? 'text-gray-400'}`}>
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
