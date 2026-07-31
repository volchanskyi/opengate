import { Link } from 'react-router';
import type { components } from '../../types/api';
import { HEALTH_META, type HealthBand, type HealthMeta } from './health';

type FleetHealthCounts = components['schemas']['FleetHealthCounts'];

/**
 * Fleet-aggregate edge-health overview. The bands are counted server-side
 * inside the time-series store and arrive as four integers, so this stays the
 * same size and the same cost whatever the fleet size.
 */
export function FleetHealth({ counts }: { readonly counts: FleetHealthCounts }) {
  // Display order (worst first, "no data" last). Every band is read through a
  // literal property access, never a computed key.
  const cards: { band: HealthBand; meta: HealthMeta; count: number }[] = [
    { band: 'anomalous', meta: HEALTH_META.anomalous, count: counts.anomalous },
    { band: 'watch', meta: HEALTH_META.watch, count: counts.watch },
    { band: 'healthy', meta: HEALTH_META.healthy, count: counts.healthy },
    { band: 'unknown', meta: HEALTH_META.unknown, count: counts.unknown },
  ];
  const monitored = counts.anomalous + counts.watch + counts.healthy;

  const bars = [
    { label: 'anomalous', count: counts.anomalous, dotClass: HEALTH_META.anomalous.dotClass },
    { label: 'watch', count: counts.watch, dotClass: HEALTH_META.watch.dotClass },
    { label: 'healthy', count: counts.healthy, dotClass: HEALTH_META.healthy.dotClass },
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
            {cards.map(({ band, meta, count }) => (
              <Link
                key={band}
                to={`/devices?health=${band}`}
                className="bg-gray-800 border border-gray-700 rounded-lg p-3 hover:bg-gray-700 transition-colors block"
              >
                <p className="text-xs text-gray-400 flex items-center gap-1">
                  <span className={`w-2 h-2 rounded-full ${meta.dotClass}`} aria-hidden="true" />
                  {meta.label}
                </p>
                <p className="text-xl font-bold mt-1">{count}</p>
              </Link>
            ))}
          </div>
        </>
      )}
    </section>
  );
}
