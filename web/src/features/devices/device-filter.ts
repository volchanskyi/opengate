import type { components } from '../../types/api';
import { healthBand, HEALTH_META, type HealthBand } from './health';

type Device = components['schemas']['Device'];

/**
 * A narrowing applied to the device list, deep-linked from the Dashboard and
 * Fleet-Health cards via `/devices?status=…&maintenance=…&health=…`. Each field
 * is optional; an absent field imposes no constraint and multiple fields AND
 * together. The reducer is pure so it can be unit-tested independently of the
 * router and composed alongside the existing keyword search.
 */
export interface DeviceFilter {
  /** `online` := status === 'online'; `offline` := status !== 'online' (includes connecting). */
  status?: 'online' | 'offline';
  /** When true, keep only devices currently in maintenance. */
  maintenance?: boolean;
  /** Keep only devices whose latest anomaly rate falls in this health band. */
  health?: HealthBand;
}

const HEALTH_BANDS = new Set<HealthBand>(['healthy', 'watch', 'anomalous', 'unknown']);

function matchesStatus(device: Device, status: DeviceFilter['status']): boolean {
  if (status === 'online') return device.status === 'online';
  if (status === 'offline') return device.status !== 'online';
  return true;
}

/** Filter `devices` down to those satisfying every set field of `filter`. */
export function applyDeviceFilter(devices: readonly Device[], filter: DeviceFilter): Device[] {
  return devices.filter((d) => {
    if (!matchesStatus(d, filter.status)) return false;
    if (filter.maintenance && d.maintenance_on !== true) return false;
    if (filter.health && healthBand(d.anomaly_rate) !== filter.health) return false;
    return true;
  });
}

/** Build a `DeviceFilter` from URL search params, dropping any unrecognized value. */
export function parseDeviceFilter(params: URLSearchParams): DeviceFilter {
  const filter: DeviceFilter = {};
  const status = params.get('status');
  if (status === 'online' || status === 'offline') filter.status = status;
  if (params.get('maintenance') === 'true') filter.maintenance = true;
  const health = params.get('health');
  if (health && HEALTH_BANDS.has(health as HealthBand)) filter.health = health as HealthBand;
  return filter;
}

/** Whether any narrowing is in effect. */
export function isDeviceFilterActive(filter: DeviceFilter): boolean {
  return filter.status !== undefined || filter.maintenance === true || filter.health !== undefined;
}

/** A short human-readable label for the active filter, or null when inactive. */
export function describeDeviceFilter(filter: DeviceFilter): string | null {
  if (filter.status === 'online') return 'Online';
  if (filter.status === 'offline') return 'Offline';
  if (filter.maintenance) return 'In maintenance';
  if (filter.health) return HEALTH_META[filter.health].label;
  return null;
}
