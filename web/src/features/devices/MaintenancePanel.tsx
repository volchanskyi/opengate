import { useState } from 'react';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import {
  daysInMaintenance,
  maintenanceSeverity,
  maintenanceDaysLabel,
  formatMaintenanceSince,
  MAINTENANCE_META,
} from './maintenance';

type Device = components['schemas']['Device'];

interface MaintenancePanelProps {
  readonly device: Device;
  /** Persist the desired maintenance state; resolves true on success. */
  readonly onToggle: (enabled: boolean, reason?: string) => Promise<boolean>;
}

/**
 * Device-detail control for the maintenance toggle. When active it offers an
 * optional-reason entry; when suppressed it states since-when, surfaces the
 * operator reason, and escalates a day-counting alert (the visible stand-in for
 * the deliberate absence of auto-expiry) before offering to resume.
 */
export function MaintenancePanel({ device, onToggle }: MaintenancePanelProps) {
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const submit = async (enabled: boolean) => {
    setSubmitting(true);
    const trimmed = reason.trim();
    const ok = await onToggle(enabled, enabled && trimmed ? trimmed : undefined);
    if (ok && enabled) setReason('');
    setSubmitting(false);
  };

  if (device.maintenance_on) {
    const days = daysInMaintenance(device.maintenance_since);
    const meta = MAINTENANCE_META[maintenanceSeverity(days)];
    const sinceLabel = formatMaintenanceSince(device.maintenance_since);
    return (
      <div>
        <h3 className="text-sm font-semibold text-gray-300 mb-2">Maintenance</h3>
        <div className={`rounded-lg p-3 ${meta.badgeClass}`}>
          <p className="text-sm">
            In maintenance{sinceLabel ? ` since ${sinceLabel}` : ''}. Telemetry and alerting are
            suppressed; remote management stays available.
          </p>
          {device.maintenance_reason && (
            <p className="text-xs mt-1 opacity-90">Reason: {device.maintenance_reason}</p>
          )}
          {days >= 1 && (
            <p role="alert" className={`text-xs mt-2 font-medium ${meta.textClass}`}>
              This device has been in maintenance for {maintenanceDaysLabel(days)} — confirm the work
              is still in progress, or resume it to restore monitoring.
            </p>
          )}
        </div>
        <button
          type="button"
          onClick={() => { fireAndForget(submit(false)); }}
          disabled={submitting}
          className="mt-2 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 rounded text-xs font-medium disabled:opacity-50"
        >
          {submitting ? 'Resuming…' : 'Exit Maintenance'}
        </button>
      </div>
    );
  }

  return (
    <div>
      <h3 className="text-sm font-semibold text-gray-300 mb-2">Maintenance</h3>
      <p className="text-xs text-gray-500 mb-2">
        Suppress telemetry and alerting while you make disruptive changes on this host. The device
        keeps its control connection, so remote management stays available and you can resume it here.
      </p>
      <div className="flex gap-2">
        <input
          type="text"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder="Reason (optional)"
          aria-label="Maintenance reason"
          className="bg-gray-900 border border-gray-600 rounded px-3 py-1.5 text-sm flex-1"
        />
        <button
          type="button"
          onClick={() => { fireAndForget(submit(true)); }}
          disabled={submitting}
          className="px-3 py-1.5 bg-amber-600 hover:bg-amber-700 rounded text-xs font-medium disabled:opacity-50 whitespace-nowrap"
        >
          {submitting ? 'Entering…' : 'Enter Maintenance'}
        </button>
      </div>
    </div>
  );
}
