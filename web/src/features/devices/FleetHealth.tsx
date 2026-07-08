import { useMemo } from 'react';
import type { components } from '../../types/api';
import { healthBand, HEALTH_META, type HealthBand } from './health';

type Device = components['schemas']['Device'];

// Display order (worst first, "no data" last), built from literal meta accesses
// so no band is read through a bare-identifier computed key.
const CARDS = [
  { band: 'anomalous' as HealthBand, meta: HEALTH_META.anomalous },
  { band: 'watch' as HealthBand, meta: HEALTH_META.watch },
  { band: 'healthy' as HealthBand, meta: HEALTH_META.healthy },
  { band: 'unknown' as HealthBand, meta: HEALTH_META.unknown },
];

/**
 * Fleet-aggregate edge-health overview, derived entirely from each device's
 * latest anomaly rate — no per-device series, so it stays cheap at fleet scale.
 * A true fleet time-series overview would need a dedicated aggregate endpoint;
 * this is the honest client-side rollup of what the device list already carries.
 */
export function FleetHealth({ devices }: { readonly devices: readonly Device[] }) {
  const counts = useMemo(() => {
    const c = new Map<HealthBand, number>();
    for (const d of devices) {
      const band = healthBand(d.anomaly_rate);
      c.set(band, (c.get(band) ?? 0) + 1);
    }
    return c;
  }, [devices]);

  const anomalous = counts.get('anomalous') ?? 0;
  const watch = counts.get('watch') ?? 0;
  const healthy = counts.get('healthy') ?? 0;
  const monitored = anomalous + watch + healthy;

  const bars = [
    { label: 'anomalous', count: anomalous, dotClass: HEALTH_META.anomalous.dotClass },
    { label: 'watch', count: watch, dotClass: HEALTH_META.watch.dotClass },
    { label: 'healthy', count: healthy, dotClass: HEALTH_META.healthy.dotClass },
  ];

  return (
    <section>
      <h3 className="text-lg font-semibold mb-3">Fleet Health</h3>
      {monitored === 0 ? (
        <p className="text-sm text-gray-500">No edge telemetry yet.</p>
      ) : (
        <>
          <figure className="flex h-2 rounded overflow-hidden mb-3" aria-label="Fleet health distribution">
            {bars.map((bar) => (bar.count > 0 ? (
              <div key={bar.label} className={bar.dotClass} style={{ width: `${String((bar.count / monitored) * 100)}%` }} />
            ) : null))}
          </figure>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            {CARDS.map(({ band, meta }) => (
              <div key={band} className="bg-gray-800 border border-gray-700 rounded-lg p-3">
                <p className="text-xs text-gray-400 flex items-center gap-1">
                  <span className={`w-2 h-2 rounded-full ${meta.dotClass}`} aria-hidden="true" />
                  {meta.label}
                </p>
                <p className="text-xl font-bold mt-1">{counts.get(band) ?? 0}</p>
              </div>
            ))}
          </div>
        </>
      )}
    </section>
  );
}
