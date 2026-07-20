import {
  daysInMaintenance,
  maintenanceSeverity,
  maintenanceDaysLabel,
  formatMaintenanceSince,
  MAINTENANCE_META,
} from './maintenance';

interface MaintenanceBadgeProps {
  /** When the device entered maintenance; drives the escalation colour + tooltip. */
  readonly since?: string | null;
  readonly className?: string;
}

/**
 * Distinct "in maintenance" pill. The fill colour escalates with the age of the
 * window (sky → amber → red) so a device left in maintenance too long stands out
 * in the list — the visible stand-in for the deliberate absence of auto-expiry.
 */
export function MaintenanceBadge({ since, className = '' }: MaintenanceBadgeProps) {
  const days = daysInMaintenance(since);
  const meta = MAINTENANCE_META[maintenanceSeverity(days)];
  const sinceLabel = formatMaintenanceSince(since);
  const title = sinceLabel
    ? `In maintenance since ${sinceLabel}${days >= 1 ? ` (for ${maintenanceDaysLabel(days)})` : ''}`
    : 'In maintenance';

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 text-xs font-medium ${meta.badgeClass} ${className}`}
      title={title}
    >
      <span className={`w-1.5 h-1.5 rounded-full ${meta.dotClass}`} aria-hidden="true" />
      Maintenance
    </span>
  );
}
