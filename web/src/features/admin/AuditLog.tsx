import { useEffect, useRef, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useAdminStore } from './state/admin-store';
import { fireAndForget } from '../../lib/fire-and-forget';

// Estimated rendered height of one audit-log table row. The virtualizer uses it
// to size the spacer rows; exact precision is not required.
const AUDIT_ROW_HEIGHT = 41;

export function AuditLog() {
  const auditEvents = useAdminStore((s) => s.auditEvents);
  const isLoading = useAdminStore((s) => s.isLoading);
  const fetchAuditEvents = useAdminStore((s) => s.fetchAuditEvents);

  const [actionFilter, setActionFilter] = useState('');
  const [offset, setOffset] = useState(0);
  const limit = 50;

  const scrollParentRef = useRef<HTMLDivElement>(null);
  const rowVirtualizer = useVirtualizer({
    count: auditEvents.length,
    getScrollElement: () => scrollParentRef.current,
    estimateSize: () => AUDIT_ROW_HEIGHT,
    overscan: 10,
  });
  const virtualRows = rowVirtualizer.getVirtualItems();
  // Spacer-row heights preserve native <table> column layout while only the
  // windowed rows are mounted (absolute-positioned <tr>s would break <td> sizing).
  const firstRow = virtualRows.at(0);
  const lastRow = virtualRows.at(-1);
  const paddingTop = firstRow ? firstRow.start : 0;
  const paddingBottom = lastRow ? rowVirtualizer.getTotalSize() - lastRow.end : 0;

  useEffect(() => {
    fireAndForget(fetchAuditEvents({
      limit,
      offset,
      ...(actionFilter ? { action: actionFilter } : {}),
    }));
  }, [fetchAuditEvents, actionFilter, offset]);

  return (
    <div>
      <h2 className="text-xl font-bold mb-4">Audit Log</h2>

      <div className="mb-4 flex gap-2">
        <input
          type="text"
          value={actionFilter}
          onChange={(e) => { setActionFilter(e.target.value); setOffset(0); }}
          placeholder="Filter by action..."
          className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-white placeholder-gray-500"
        />
      </div>

      {isLoading && auditEvents.length === 0 ? (
        <p className="text-gray-400">Loading audit events...</p>
      ) : (
        <>
          {/* Virtualized rows: only the windowed events are mounted; spacer
              rows reserve the scroll height so the native table layout holds. */}
          <div ref={scrollParentRef} className="max-h-[calc(100vh-220px)] overflow-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-700 text-left text-gray-400">
                  <th className="pb-2">Time</th>
                  <th className="pb-2">Action</th>
                  <th className="pb-2">Target</th>
                  <th className="pb-2">User ID</th>
                  <th className="pb-2">Details</th>
                </tr>
              </thead>
              <tbody>
                {paddingTop > 0 && (
                  <tr>
                    <td colSpan={5} style={{ height: paddingTop }} />
                  </tr>
                )}
                {virtualRows.map((virtualRow) => {
                  const event = auditEvents[virtualRow.index];
                  if (!event) return null;
                  return (
                    <tr key={event.id} className="border-b border-gray-800">
                      <td className="py-2 text-gray-400">{new Date(event.created_at).toLocaleString()}</td>
                      <td className="py-2">
                        <span className="px-2 py-0.5 rounded bg-gray-700 text-xs">{event.action}</span>
                      </td>
                      <td className="py-2 font-mono text-xs">{event.target}</td>
                      <td className="py-2 font-mono text-xs text-gray-400">{event.user_id.slice(0, 8)}</td>
                      <td className="py-2 text-gray-400">{event.details}</td>
                    </tr>
                  );
                })}
                {paddingBottom > 0 && (
                  <tr>
                    <td colSpan={5} style={{ height: paddingBottom }} />
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          <div className="mt-4 flex gap-2">
            <button
              onClick={() => setOffset(Math.max(0, offset - limit))}
              disabled={offset === 0}
              className="px-3 py-1 text-sm bg-gray-800 rounded disabled:opacity-50"
            >
              Previous
            </button>
            <button
              onClick={() => setOffset(offset + limit)}
              disabled={auditEvents.length < limit}
              className="px-3 py-1 text-sm bg-gray-800 rounded disabled:opacity-50"
            >
              Next
            </button>
          </div>
        </>
      )}
    </div>
  );
}
