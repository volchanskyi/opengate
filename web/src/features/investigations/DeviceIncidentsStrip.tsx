import { useCallback, useEffect } from 'react';
import { Link } from 'react-router';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useVisibleInterval } from '../../lib/use-visible-interval';
import { IncidentSeverityBadge, IncidentStatusBadge } from './IncidentBadges';
import { countLabel } from './incident-format';
import { useQueueStore } from './state/queue-store';

/** How often a machine's page re-reads what it is caught up in. */
const STRIP_POLL_MS = 60_000;

/**
 * The open incidents one machine is in, as a strip on that machine's page —
 * including the customer-wide ones it is one of forty machines in.
 *
 * A machine with nothing open renders nothing at all: an empty box on every
 * healthy device's page is noise that trains people to stop reading the space.
 */
export function DeviceIncidentsStrip({ deviceId, className = '' }: {
  readonly deviceId: string;
  /** Layout the host page needs on the strip itself, so nothing is laid out when there is no strip. */
  readonly className?: string;
}) {
  const incidents = useQueueStore((s) => s.byDevice.get(deviceId));
  const fetchDeviceIncidents = useQueueStore((s) => s.fetchDeviceIncidents);

  const load = useCallback(() => {
    fireAndForget(fetchDeviceIncidents(deviceId));
  }, [deviceId, fetchDeviceIncidents]);

  useEffect(() => { load(); }, [load]);
  useVisibleInterval(load, STRIP_POLL_MS);

  if (!incidents || incidents.length === 0) return null;

  return (
    <section
      aria-label="Open incidents"
      className={`bg-gray-800 border border-amber-700/60 rounded-lg p-3 space-y-2 ${className}`}
    >
      <h3 className="text-xs font-semibold text-amber-300">
        {countLabel(incidents.length, 'open incident', 'open incidents')}
      </h3>
      <ul className="space-y-1">
        {incidents.map((incident) => (
          <li key={incident.id} className="flex items-center gap-2 flex-wrap">
            <IncidentSeverityBadge severity={incident.severity} />
            <IncidentStatusBadge status={incident.status} />
            <Link to={`/investigations/${incident.id}`} className="text-sm text-blue-400 hover:text-blue-300">
              {incident.rule_id}
            </Link>
            <span className="text-xs text-gray-500">
              {countLabel(incident.occurrences, 'alert', 'alerts')} · {countLabel(incident.device_count, 'machine', 'machines')}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
