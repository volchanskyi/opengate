import { describe, it, expect } from 'vitest';
import {
  daysInMaintenance,
  maintenanceSeverity,
  maintenanceDaysLabel,
  formatMaintenanceSince,
  MAINTENANCE_META,
  MAINTENANCE_WARN_DAYS,
  MAINTENANCE_STALE_DAYS,
} from './maintenance';

const DAY = 86_400_000;
const NOW = new Date('2026-07-19T12:00:00Z').getTime();

describe('daysInMaintenance', () => {
  it('returns 0 for a missing timestamp', () => {
    expect(daysInMaintenance(undefined, NOW)).toBe(0);
    expect(daysInMaintenance(null, NOW)).toBe(0);
    expect(daysInMaintenance('', NOW)).toBe(0);
  });

  it('returns 0 for an unparseable timestamp', () => {
    expect(daysInMaintenance('not-a-date', NOW)).toBe(0);
  });

  it('returns 0 for a window shorter than 24h', () => {
    expect(daysInMaintenance(new Date(NOW - 23 * 3_600_000).toISOString(), NOW)).toBe(0);
  });

  it('returns 1 exactly at 24h — kills the floor/boundary mutant', () => {
    expect(daysInMaintenance(new Date(NOW - DAY).toISOString(), NOW)).toBe(1);
  });

  it('floors partial days (2.9 days → 2)', () => {
    expect(daysInMaintenance(new Date(NOW - Math.floor(2.9 * DAY)).toISOString(), NOW)).toBe(2);
  });

  it('clamps a future timestamp (clock skew) to 0, never negative', () => {
    expect(daysInMaintenance(new Date(NOW + 5 * DAY).toISOString(), NOW)).toBe(0);
  });
});

describe('maintenanceSeverity', () => {
  it('is normal below the warn threshold', () => {
    expect(maintenanceSeverity(0)).toBe('normal');
    expect(maintenanceSeverity(MAINTENANCE_WARN_DAYS - 1)).toBe('normal');
  });

  it('escalates to warn exactly at the warn threshold', () => {
    expect(maintenanceSeverity(MAINTENANCE_WARN_DAYS)).toBe('warn');
    expect(maintenanceSeverity(MAINTENANCE_STALE_DAYS - 1)).toBe('warn');
  });

  it('escalates to stale exactly at the stale threshold', () => {
    expect(maintenanceSeverity(MAINTENANCE_STALE_DAYS)).toBe('stale');
    expect(maintenanceSeverity(MAINTENANCE_STALE_DAYS + 10)).toBe('stale');
  });

  it('orders thresholds warn < stale', () => {
    expect(MAINTENANCE_WARN_DAYS).toBeLessThan(MAINTENANCE_STALE_DAYS);
  });
});

describe('maintenanceDaysLabel', () => {
  it('singularizes exactly one day', () => {
    expect(maintenanceDaysLabel(1)).toBe('1 day');
  });

  it('pluralizes multiple days', () => {
    expect(maintenanceDaysLabel(5)).toBe('5 days');
  });

  it('pluralizes zero days', () => {
    expect(maintenanceDaysLabel(0)).toBe('0 days');
  });
});

describe('formatMaintenanceSince', () => {
  it('returns an empty string for a missing timestamp', () => {
    expect(formatMaintenanceSince(undefined)).toBe('');
    expect(formatMaintenanceSince('')).toBe('');
  });

  it('returns an empty string for an unparseable timestamp', () => {
    expect(formatMaintenanceSince('not-a-date')).toBe('');
  });

  it('renders a parseable timestamp as a locale string', () => {
    const since = new Date(NOW).toISOString();
    expect(formatMaintenanceSince(since)).toBe(new Date(since).toLocaleString());
  });
});

describe('MAINTENANCE_META', () => {
  it('has a distinct meta entry per severity', () => {
    const metas = [MAINTENANCE_META.normal, MAINTENANCE_META.warn, MAINTENANCE_META.stale];
    for (const m of metas) {
      expect(m.dotClass).toBeTruthy();
      expect(m.textClass).toBeTruthy();
      expect(m.badgeClass).toBeTruthy();
    }
    // Escalation must be visually distinct, not the same colour repeated.
    expect(new Set(metas.map((m) => m.textClass)).size).toBe(3);
  });
});
