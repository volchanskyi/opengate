import { describe, it, expect } from 'vitest';
import type { components } from '../../types/api';
import {
  applyDeviceFilter,
  parseDeviceFilter,
  describeDeviceFilter,
  isDeviceFilterActive,
  type DeviceFilter,
} from './device-filter';

type Device = components['schemas']['Device'];

function dev(overrides: Partial<Device>): Device {
  return {
    id: 'd', organization_id: 'org-1', group_id: 'g', hostname: 'h', os: 'linux', agent_version: '1.0.0',
    capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '',
    ...overrides,
  };
}

const online = dev({ id: 'on', status: 'online' });
const offline = dev({ id: 'off', status: 'offline' });
const connecting = dev({ id: 'conn', status: 'connecting' });
const maint = dev({ id: 'm', maintenance_on: true });
const anomalous = dev({ id: 'a', anomaly_rate: 0.9 });
const watch = dev({ id: 'w', anomaly_rate: 0.15 });
const healthy = dev({ id: 'hh', anomaly_rate: 0.01 });
const noData = dev({ id: 'nd' });

describe('applyDeviceFilter', () => {
  const all = [online, offline, connecting, maint, anomalous, watch, healthy, noData];

  it('returns every device when no filter is set', () => {
    expect(applyDeviceFilter(all, {})).toEqual(all);
  });

  it('status=online keeps only online devices', () => {
    // maint/anomalous/watch/healthy/noData all default to status 'online'.
    expect(applyDeviceFilter(all, { status: 'online' }).map((d) => d.id))
      .toEqual(['on', 'm', 'a', 'w', 'hh', 'nd']);
  });

  it('status=offline keeps everything that is not online (includes connecting)', () => {
    // Mirrors the Dashboard "Offline" count (total − online), which includes connecting.
    expect(applyDeviceFilter(all, { status: 'offline' }).map((d) => d.id))
      .toEqual(['off', 'conn']);
  });

  it('maintenance=true keeps only devices in maintenance', () => {
    expect(applyDeviceFilter(all, { maintenance: true }).map((d) => d.id)).toEqual(['m']);
  });

  it('health=anomalous keeps only anomalous-band devices', () => {
    expect(applyDeviceFilter(all, { health: 'anomalous' }).map((d) => d.id)).toEqual(['a']);
  });

  it('health=watch keeps only watch-band devices', () => {
    expect(applyDeviceFilter(all, { health: 'watch' }).map((d) => d.id)).toEqual(['w']);
  });

  it('health=healthy keeps only healthy-band devices', () => {
    // healthy band = a rate below the watch threshold. online (no rate) is unknown, not healthy.
    expect(applyDeviceFilter(all, { health: 'healthy' }).map((d) => d.id)).toEqual(['hh']);
  });

  it('health=unknown keeps only devices without a finite anomaly rate', () => {
    expect(applyDeviceFilter(all, { health: 'unknown' }).map((d) => d.id))
      .toEqual(['on', 'off', 'conn', 'm', 'nd']);
  });

  it('composes status and health (AND semantics)', () => {
    const anomalousOffline = dev({ id: 'ao', status: 'offline', anomaly_rate: 0.9 });
    const list = [anomalous, anomalousOffline];
    expect(applyDeviceFilter(list, { status: 'offline', health: 'anomalous' }).map((d) => d.id))
      .toEqual(['ao']);
  });
});

describe('parseDeviceFilter', () => {
  it('reads status/maintenance/health from search params', () => {
    expect(parseDeviceFilter(new URLSearchParams('status=online'))).toEqual({ status: 'online' });
    expect(parseDeviceFilter(new URLSearchParams('status=offline'))).toEqual({ status: 'offline' });
    expect(parseDeviceFilter(new URLSearchParams('maintenance=true'))).toEqual({ maintenance: true });
    expect(parseDeviceFilter(new URLSearchParams('health=anomalous'))).toEqual({ health: 'anomalous' });
  });

  it('ignores unknown / garbage parameter values', () => {
    expect(parseDeviceFilter(new URLSearchParams('status=bogus'))).toEqual({});
    expect(parseDeviceFilter(new URLSearchParams('health=purple'))).toEqual({});
    expect(parseDeviceFilter(new URLSearchParams('maintenance=nope'))).toEqual({});
    expect(parseDeviceFilter(new URLSearchParams(''))).toEqual({});
  });

  it('applying a garbage-param filter is a no-op (no devices removed)', () => {
    const all = [online, offline];
    expect(applyDeviceFilter(all, parseDeviceFilter(new URLSearchParams('status=bogus')))).toEqual(all);
  });
});

describe('isDeviceFilterActive / describeDeviceFilter', () => {
  it('reports inactive for an empty filter', () => {
    expect(isDeviceFilterActive({})).toBe(false);
    expect(describeDeviceFilter({})).toBeNull();
  });

  it('reports active and a human label for each filter kind', () => {
    const cases: [DeviceFilter, string][] = [
      [{ status: 'online' }, 'Online'],
      [{ status: 'offline' }, 'Offline'],
      [{ maintenance: true }, 'In maintenance'],
      [{ health: 'anomalous' }, 'Anomalous'],
      [{ health: 'watch' }, 'Watch'],
      [{ health: 'healthy' }, 'Healthy'],
      [{ health: 'unknown' }, 'No data'],
    ];
    for (const [filter, label] of cases) {
      expect(isDeviceFilterActive(filter)).toBe(true);
      expect(describeDeviceFilter(filter)).toBe(label);
    }
  });
});
