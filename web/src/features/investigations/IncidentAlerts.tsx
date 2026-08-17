import { useState } from 'react';
import { Link } from 'react-router';
import type { components } from '../../types/api';
import { shortId } from '../../lib/short-id';
import { formatBytes } from '../../lib/format-bytes';
import { fireAndForget } from '../../lib/fire-and-forget';
import { AlertEvidencePanel } from './AlertEvidencePanel';
import { IncidentSeverityBadge } from './IncidentBadges';
import { alertReadingLabel, formatMoment } from './incident-format';
import { useRoomStore } from './state/room-store';

type IncidentAlert = components['schemas']['IncidentAlert'];

interface Props {
  readonly incidentId: string;
  readonly alerts: readonly IncidentAlert[];
  /** How many alerts the incident holds altogether. */
  readonly total: number;
  /** How many machines the incident covers, as the incident itself counts them. */
  readonly deviceCount: number;
}

const HEAD_CELL = 'px-3 py-2 text-left text-xs font-semibold text-gray-400';
const CELL = 'px-3 py-2 text-sm text-gray-300 align-top';

function AlertRow({ incidentId, alert }: { readonly incidentId: string; readonly alert: IncidentAlert }) {
  const [open, setOpen] = useState(false);
  const evidence = useRoomStore((s) => s.evidence.get(alert.id));
  const loading = useRoomStore((s) => s.evidenceLoading.get(alert.id) ?? false);
  const error = useRoomStore((s) => s.evidenceErrors.get(alert.id));
  const fetchEvidence = useRoomStore((s) => s.fetchEvidence);

  const toggle = () => {
    if (!open) fireAndForget(fetchEvidence(incidentId, alert.id));
    setOpen((v) => !v);
  };

  return (
    <>
      <tr className="border-t border-gray-800">
        <td className={CELL}>
          {/* A machine is named, never asked: the room reads a frozen snapshot. */}
          <Link to={`/devices/${alert.device_id}`} className="text-blue-400 hover:text-blue-300 font-mono text-xs">
            {shortId(alert.device_id)}
          </Link>
        </td>
        <td className={CELL}><IncidentSeverityBadge severity={alert.severity} /></td>
        <td className={CELL}>{alertReadingLabel(alert.metric, alert.value)}</td>
        <td className={`${CELL} whitespace-nowrap`}>{formatMoment(alert.observed_at)}</td>
        <td className={CELL}>
          {alert.backfilled && (
            <span className="px-2 py-0.5 rounded text-xs bg-indigo-900 text-indigo-200">Backfilled</span>
          )}
        </td>
        <td className={CELL}>
          {alert.evidence_bytes > 0 ? (
            <button
              type="button"
              onClick={toggle}
              aria-expanded={open}
              className="px-2 py-1 rounded text-xs bg-gray-700 hover:bg-gray-600"
            >
              {open ? 'Hide evidence' : `Show evidence (${formatBytes(alert.evidence_bytes)})`}
            </button>
          ) : (
            <span className="text-xs text-gray-500">No evidence was recorded</span>
          )}
        </td>
      </tr>
      {open && (
        <tr className="border-t border-gray-800">
          <td colSpan={6} className="px-3 pb-3">
            <AlertEvidencePanel evidence={evidence} loading={loading} error={error} />
          </td>
        </tr>
      )}
    </>
  );
}

/**
 * The alerts this room folded, and the evidence each of them carries.
 *
 * The page is bounded and the incident is not, so both counts are stated: how
 * many alerts are on screen against how many the room holds, and how many
 * machines those alerts came from against how many the incident covers. A room
 * whose alerts are fewer than its machines is a room to read, not an error.
 */
export function IncidentAlerts({ incidentId, alerts, total, deviceCount }: Props) {
  const shownDevices = new Set(alerts.map((a) => a.device_id)).size;

  return (
    <div className="space-y-2">
      <div className="flex items-baseline gap-3 flex-wrap text-xs text-gray-500">
        {total > alerts.length && <span>Showing {alerts.length} of {total} alerts.</span>}
        {shownDevices !== deviceCount && (
          <span>From {shownDevices} of {deviceCount} machines this incident covers.</span>
        )}
      </div>

      {alerts.length === 0 ? (
        <p className="text-sm text-gray-500">No alerts are held in this room.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr>
                <th className={HEAD_CELL}>Machine</th>
                <th className={HEAD_CELL}>Severity</th>
                <th className={HEAD_CELL}>Reading</th>
                <th className={HEAD_CELL}>Observed</th>
                <th className={HEAD_CELL}>Origin</th>
                <th className={HEAD_CELL}>Evidence</th>
              </tr>
            </thead>
            <tbody>
              {alerts.map((alert) => <AlertRow key={alert.id} incidentId={incidentId} alert={alert} />)}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
