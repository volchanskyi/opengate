import { useCallback, useEffect } from 'react';
import { Link } from 'react-router';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useVisibleInterval } from '../../lib/use-visible-interval';
import { useOrganizationStore } from '../organizations';
import { IncidentSeverityBadge, IncidentStatusBadge } from './IncidentBadges';
import { InvestigationFilters } from './InvestigationFilters';
import { countLabel, durationLabel, formatMoment, scopeLabel } from './incident-format';
import { useQueueStore } from './state/queue-store';

type Incident = components['schemas']['Incident'];

/** How often the queue re-reads itself while somebody is watching it. */
const QUEUE_POLL_MS = 30_000;

const HEAD_CELL = 'px-3 py-2 text-left text-xs font-semibold text-gray-400';
const CELL = 'px-3 py-2 text-sm text-gray-300 whitespace-nowrap';

function QueueRow({ incident }: { readonly incident: Incident }) {
  return (
    <tr className="border-t border-gray-800 hover:bg-gray-800">
      <td className={CELL}><IncidentSeverityBadge severity={incident.severity} /></td>
      <td className={CELL}><IncidentStatusBadge status={incident.status} /></td>
      <td className={CELL}>
        <Link to={`/investigations/${incident.id}`} className="text-blue-400 hover:text-blue-300 font-medium">
          {incident.rule_id}
        </Link>
      </td>
      <td className={CELL}>{countLabel(incident.occurrences, 'alert', 'alerts')}</td>
      <td className={CELL}>{countLabel(incident.device_count, 'host', 'hosts')}</td>
      <td className={CELL}>{durationLabel(incident.first_seen, incident.last_seen)}</td>
      <td className={CELL}>{formatMoment(incident.last_seen)}</td>
      <td className={`${CELL} text-xs text-gray-400`}>{scopeLabel(incident)}</td>
    </tr>
  );
}

/** The triage queue; every row links to the incident's room. */
export function InvestigationList() {
  const items = useQueueStore((s) => s.items);
  const loading = useQueueStore((s) => s.loading);
  const loaded = useQueueStore((s) => s.loaded);
  const error = useQueueStore((s) => s.error);
  const nextCursor = useQueueStore((s) => s.nextCursor);
  const pagedOn = useQueueStore((s) => s.pagedOn);
  const filters = useQueueStore((s) => s.filters);
  const setFilters = useQueueStore((s) => s.setFilters);
  const fetchQueue = useQueueStore((s) => s.fetchQueue);
  const fetchMore = useQueueStore((s) => s.fetchMore);
  const organizationId = useOrganizationStore((s) => s.selectedOrganizationId);

  const load = useCallback(() => { fireAndForget(fetchQueue()); }, [fetchQueue]);

  useEffect(() => { load(); }, [load, filters, organizationId]);

  // A re-read restarts at the first page, so polling stops once the reader has paged on.
  const poll = useCallback(() => { if (!pagedOn) load(); }, [pagedOn, load]);
  useVisibleInterval(poll, QUEUE_POLL_MS);

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-baseline justify-between gap-3 flex-wrap">
        <h2 className="text-xl font-bold">Investigations</h2>
        {loaded && <span className="text-sm text-gray-400">{countLabel(items.length, 'incident', 'incidents')}</span>}
      </div>

      <InvestigationFilters filters={filters} onChange={setFilters} />

      {error && (
        <p role="alert" className="text-sm text-red-400">{error}</p>
      )}

      {items.length > 0 && (
        <div className="overflow-x-auto bg-gray-800 border border-gray-700 rounded-lg">
          <table className="w-full">
            <thead>
              <tr>
                <th className={HEAD_CELL}>Severity</th>
                <th className={HEAD_CELL}>Status</th>
                <th className={HEAD_CELL}>Rule</th>
                <th className={HEAD_CELL}>Alerts</th>
                <th className={HEAD_CELL}>Hosts</th>
                <th className={HEAD_CELL}>Running for</th>
                <th className={HEAD_CELL}>Last seen</th>
                <th className={HEAD_CELL}>Scope</th>
              </tr>
            </thead>
            <tbody>
              {items.map((incident) => <QueueRow key={incident.id} incident={incident} />)}
            </tbody>
          </table>
        </div>
      )}

      {items.length === 0 && loading && (
        <p className="text-sm text-gray-400">Reading the queue…</p>
      )}

      {items.length === 0 && loaded && !loading && (
        <p className="text-sm text-gray-500">
          Nothing to work — no incident matches these filters.
        </p>
      )}

      {nextCursor !== null && (
        <button
          type="button"
          onClick={() => { fireAndForget(fetchMore()); }}
          disabled={loading}
          className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded text-xs font-medium disabled:opacity-50"
        >
          Load more
        </button>
      )}

    </div>
  );
}
