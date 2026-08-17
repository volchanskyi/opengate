import { useCallback, useEffect } from 'react';
import { Link, useParams } from 'react-router';
import { fireAndForget } from '../../lib/fire-and-forget';
import { shortId } from '../../lib/short-id';
import { useVisibleInterval } from '../../lib/use-visible-interval';
import { IncidentActions } from './IncidentActions';
import { IncidentAlerts } from './IncidentAlerts';
import { IncidentSeverityBadge, IncidentStatusBadge } from './IncidentBadges';
import { IncidentTimeline } from './IncidentTimeline';
import { countLabel, durationLabel, formatMoment } from './incident-format';
import { useRoomStore } from './state/room-store';

/** How often an open room re-reads itself while somebody is in it. */
const ROOM_POLL_MS = 30_000;

const CARD = 'bg-gray-800 border border-gray-700 rounded-lg p-6';

function BackLink() {
  return (
    <Link to="/investigations" className="text-sm text-blue-400 hover:text-blue-300">
      ← Back to the queue
    </Link>
  );
}

/**
 * One incident, as somebody working it sees it: what it is, how it got here, and
 * the alerts it folded with the evidence each one carries.
 *
 * Everything on this page comes from the incident read. The evidence was frozen
 * on the machine at the moment the alert fired and nothing can be fetched from
 * the machine afterwards, so what is not here about an event is not recorded
 * anywhere — which is why the page states an absence rather than leaving a gap.
 */
export function InvestigationDetail() {
  const { id } = useParams<{ id: string }>();
  const detail = useRoomStore((s) => s.detail);
  const loading = useRoomStore((s) => s.loading);
  const error = useRoomStore((s) => s.error);
  const open = useRoomStore((s) => s.open);
  const refresh = useRoomStore((s) => s.refresh);
  const leave = useRoomStore((s) => s.leave);

  useEffect(() => {
    if (id) fireAndForget(open(id));
    return leave;
  }, [id, open, leave]);

  const poll = useCallback(() => {
    if (id) fireAndForget(refresh(id));
  }, [id, refresh]);
  useVisibleInterval(poll, ROOM_POLL_MS);

  if (!detail) {
    return (
      <div className="p-6 space-y-3">
        <BackLink />
        {error
          ? <p role="alert" className="text-sm text-red-400">{error}</p>
          : <p className="text-sm text-gray-400">{loading ? 'Reading the incident…' : 'This incident is not open.'}</p>}
      </div>
    );
  }

  const { incident } = detail;

  return (
    <div className="p-6 space-y-4">
      <BackLink />

      <section aria-label="Incident summary" className={`${CARD} space-y-3`}>
        <div className="flex items-center gap-3 flex-wrap">
          <h2 className="text-xl font-bold">{incident.rule_id}</h2>
          <IncidentSeverityBadge severity={incident.severity} />
          <IncidentStatusBadge status={incident.status} />
        </div>

        <p className="text-sm text-gray-400">
          {[
            countLabel(incident.occurrences, 'alert', 'alerts'),
            `across ${countLabel(incident.device_count, 'machine', 'machines')}`,
            `running for ${durationLabel(incident.first_seen, incident.last_seen)}`,
          ].join(' · ')}
        </p>

        <dl className="grid grid-cols-2 lg:grid-cols-4 gap-3 text-sm">
          <div>
            <dt className="text-gray-400 text-xs">Opened</dt>
            <dd>{formatMoment(incident.opened_at)}</dd>
          </div>
          <div>
            <dt className="text-gray-400 text-xs">First seen</dt>
            <dd>{formatMoment(incident.first_seen)}</dd>
          </div>
          <div>
            <dt className="text-gray-400 text-xs">Last seen</dt>
            <dd>{formatMoment(incident.last_seen)}</dd>
          </div>
          <div>
            <dt className="text-gray-400 text-xs">Scope</dt>
            <dd className="font-mono text-xs">{incident.scope} · {shortId(incident.scope_key)}</dd>
          </div>
        </dl>

        <IncidentActions incident={incident} />
      </section>

      <section aria-label="Timeline" className={CARD}>
        <h3 className="text-sm font-semibold text-gray-300 mb-3">Timeline</h3>
        <IncidentTimeline events={detail.events} total={detail.events_total} />
      </section>

      <section aria-label="Alerts" className={CARD}>
        <h3 className="text-sm font-semibold text-gray-300 mb-3">Alerts</h3>
        <IncidentAlerts
          incidentId={incident.id}
          alerts={detail.alerts}
          total={detail.alerts_total}
          deviceCount={incident.device_count}
        />
      </section>
    </div>
  );
}
