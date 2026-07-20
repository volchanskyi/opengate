/**
 * Maintenance-mode presentation helpers. A device in maintenance suppresses
 * telemetry and alerting while remote management stays live. Because the state
 * is manual-only (no auto-expiry), a forgotten device would otherwise stay blind
 * indefinitely, so the UI compensates by surfacing the window prominently and
 * escalating a warning the longer the device stays quiet — visibility stands in
 * for the missing auto-revert safety net.
 */

export type MaintenanceSeverity = 'normal' | 'warn' | 'stale';

/** Whole days in maintenance at/after which the surfacing escalates. */
export const MAINTENANCE_WARN_DAYS = 3;
export const MAINTENANCE_STALE_DAYS = 7;

const MS_PER_DAY = 86_400_000;

/**
 * Whole days elapsed since the device entered maintenance. Sub-day windows and
 * future timestamps (clock skew) both floor to 0 — never negative.
 */
export function daysInMaintenance(
  since: string | null | undefined,
  now: number = Date.now(),
): number {
  if (!since) return 0;
  const start = new Date(since).getTime();
  if (!Number.isFinite(start)) return 0;
  const days = Math.floor((now - start) / MS_PER_DAY);
  return days > 0 ? days : 0;
}

/** Escalation band for how long a device has sat in maintenance. */
export function maintenanceSeverity(days: number): MaintenanceSeverity {
  if (days >= MAINTENANCE_STALE_DAYS) return 'stale';
  if (days >= MAINTENANCE_WARN_DAYS) return 'warn';
  return 'normal';
}

export interface MaintenanceMeta {
  /** Tailwind background class for the status dot. */
  dotClass: string;
  /** Tailwind text-colour class for escalation copy. */
  textClass: string;
  /** Tailwind classes for a filled badge/pill. */
  badgeClass: string;
}

export const MAINTENANCE_META: Record<MaintenanceSeverity, MaintenanceMeta> = {
  normal: {
    dotClass: 'bg-sky-500',
    textClass: 'text-sky-400',
    badgeClass: 'bg-sky-900/40 text-sky-300 border border-sky-700',
  },
  warn: {
    dotClass: 'bg-amber-500',
    textClass: 'text-amber-400',
    badgeClass: 'bg-amber-900/40 text-amber-300 border border-amber-700',
  },
  stale: {
    dotClass: 'bg-red-500',
    textClass: 'text-red-400',
    badgeClass: 'bg-red-900/40 text-red-300 border border-red-700',
  },
};

/** Pluralized day count, e.g. "1 day" / "5 days". */
export function maintenanceDaysLabel(days: number): string {
  return `${String(days)} day${days === 1 ? '' : 's'}`;
}

/** Locale-string rendering of a maintenance start timestamp, or '' when absent/invalid. */
export function formatMaintenanceSince(since: string | null | undefined): string {
  if (!since) return '';
  const d = new Date(since);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString();
}
