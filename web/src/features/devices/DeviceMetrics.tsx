import { useCallback, useEffect, useMemo, useState } from 'react';
import type { components } from '../../types/api';
import { useDeviceStore } from './state/device-store';
import { TimeSeriesChart } from './charts/TimeSeriesChart';
import { buildFamilyChart, familyCurrentLabel, groupByFamily } from './charts/aligned-data';
import { HealthBadge } from './HealthBadge';
import { healthBand, HEALTH_META } from './health';
import { formatMaintenanceSince } from './maintenance';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useVisibleInterval } from '../../lib/use-visible-interval';

type MinMaxSource = components['schemas']['MetricSeries']['min_max_source'];

const MAX_POINTS = 1000;
/** A device writes its vitals every 60 s; reading faster than that re-reads the
 *  same window. */
const POLL_MS = 60_000;

const PRESETS = [
  { key: '1h', seconds: 3600 },
  { key: '6h', seconds: 6 * 3600 },
  { key: '24h', seconds: 24 * 3600 },
  { key: '7d', seconds: 7 * 24 * 3600 },
] as const;

const DEFAULT_PRESET = '6h';

/** Honest description of what a family's band represents (central VM is avg-only).
 *  Surfaced as a header `title` tooltip rather than an inline caption. */
function bandProvenance(hasBand: boolean, source: MinMaxSource): string {
  if (!hasBand) return 'avg only';
  if (source === 'local') return 'Band: host min/max (local history)';
  if (source === 'avg_of_60s') return 'Band: min/max across 60 s averages (not host extrema)';
  return 'avg only';
}

function AnomalyPanel({ anomalyRate, maintenanceSince }: { readonly anomalyRate: number | null | undefined; readonly maintenanceSince?: string | null }) {
  if (maintenanceSince) {
    const sinceLabel = formatMaintenanceSince(maintenanceSince);
    return (
      <div className="flex items-center gap-3">
        <div>
          <p className="text-xs text-gray-400">Edge health</p>
          <p className="text-lg font-bold text-sky-400">In maintenance</p>
        </div>
        {sinceLabel && <span className="text-xs text-gray-400">since {sinceLabel}</span>}
      </div>
    );
  }
  const meta = HEALTH_META[healthBand(anomalyRate)];
  return (
    <div className="flex items-center gap-3">
      <div>
        <p className="text-xs text-gray-400">Edge health</p>
        <p className={`text-2xl font-bold ${meta.textClass}`}>
          <HealthBadge anomalyRate={anomalyRate} showPct />
        </p>
      </div>
      <span className={`text-xs ${meta.textClass}`}>{meta.label}</span>
    </div>
  );
}

/** Non-chart states for the metrics area: paused-by-maintenance, empty, or loading. */
function MetricsPlaceholder({ hasMetrics, loading, maintenanceSince }: {
  readonly hasMetrics: boolean;
  readonly loading: boolean;
  readonly maintenanceSince?: string | null;
}) {
  if (hasMetrics) {
    if (maintenanceSince) {
      const sinceLabel = formatMaintenanceSince(maintenanceSince);
      return (
        <p className="text-xs text-gray-500">
          In maintenance{sinceLabel ? ` since ${sinceLabel}` : ''} — telemetry is paused and resumes when the device exits maintenance.
        </p>
      );
    }
    return <p className="text-xs text-gray-500">No telemetry recorded for this window.</p>;
  }
  if (loading) return <p className="text-xs text-gray-400">Loading metrics…</p>;
  return null;
}

interface DeviceMetricsProps {
  readonly deviceId: string;
  readonly anomalyRate?: number | null;
  /** When set, the device is in maintenance: telemetry is paused, so the panel
   *  shows the since-when state rather than a stale health band or "no data". */
  readonly maintenanceSince?: string | null;
  /** Opens the logs explorer for the charted window (unix seconds). */
  readonly onViewLogs?: (fromSec: number, toSec: number) => void;
}

/**
 * Device-detail telemetry panel: an anomaly summary and per-family metric
 * timelines (avg line + honest provenance band). Ranking of which dimensions
 * broke pattern arrives with the alert the agent raises, so this panel is a
 * read of the window rather than a place to ask a question of it. All heavy
 * rendering is delegated to the imperative chart adapter.
 */
export function DeviceMetrics({ deviceId, anomalyRate, maintenanceSince, onViewLogs }: DeviceMetricsProps) {
  const metrics = useDeviceStore((s) => s.metrics);
  const metricsLoading = useDeviceStore((s) => s.metricsLoading);
  const fetchMetrics = useDeviceStore((s) => s.fetchMetrics);

  const [presetKey, setPresetKey] = useState<string>(DEFAULT_PRESET);
  const seconds = PRESETS.find((p) => p.key === presetKey)?.seconds ?? PRESETS[1].seconds;

  const load = useCallback(() => {
    const to = new Date();
    const from = new Date(to.getTime() - seconds * 1000);
    fireAndForget(fetchMetrics(deviceId, {
      from: from.toISOString(),
      to: to.toISOString(),
      band: 'avg_of_60s',
      maxPoints: MAX_POINTS,
    }));
  }, [deviceId, seconds, fetchMetrics]);

  useEffect(() => { load(); }, [load]);

  // Keep the window fresh without re-running the React reconciler over points —
  // the adapter pushes new data through setData.
  useVisibleInterval(load, POLL_MS);

  const handleViewLogs = useCallback(() => {
    if (!onViewLogs) return;
    const toSec = Math.floor(Date.now() / 1000);
    onViewLogs(toSec - seconds, toSec);
  }, [onViewLogs, seconds]);

  // System-metric families exclude the `log` family: its dimensions are log
  // volume/severity counts, not a system resource, so they are not charted here.
  const families = useMemo(() => {
    if (!metrics) return [];
    return [...groupByFamily(metrics.series).entries()]
      .filter(([family]) => family !== 'log')
      .map(([family, series]) => {
        const chart = buildFamilyChart(metrics.t, series);
        return {
          family,
          chart,
          source: series[0]?.min_max_source ?? 'none' as MinMaxSource,
          current: familyCurrentLabel(series),
        };
      });
  }, [metrics]);

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <AnomalyPanel anomalyRate={anomalyRate} maintenanceSince={maintenanceSince} />
        <div className="flex items-center gap-2">
          {onViewLogs && (
            <button
              type="button"
              onClick={handleViewLogs}
              className="px-2 py-1 rounded text-xs bg-yellow-600 hover:bg-yellow-700"
            >
              View logs for this window
            </button>
          )}
          <div className="flex gap-1" role="site" aria-label="Metrics window">
            {PRESETS.map((p) => (
              <button
                key={p.key}
                type="button"
                onClick={() => setPresetKey(p.key)}
                className={`px-2 py-1 rounded text-xs ${presetKey === p.key ? 'bg-blue-600 text-white' : 'bg-gray-700 text-gray-300 hover:bg-gray-600'}`}
              >
                {p.key}
              </button>
            ))}
          </div>
        </div>
      </div>

      {families.length > 0 ? (
        families.map(({ family, chart, source, current }) => (
          <div key={family}>
            <div
              className="flex items-baseline gap-2"
              title={bandProvenance(chart.bands.length > 0, source)}
            >
              <h4 className="text-xs font-semibold text-gray-300 capitalize">{family}</h4>
              {current && <span className="text-sm font-bold text-gray-100 tabular-nums">{current}</span>}
            </div>
            <TimeSeriesChart
              data={chart.data}
              series={chart.series}
              bands={chart.bands}
              yRange={chart.scaleRange}
              height={160}
              ariaLabel={`${family} metrics`}
            />
          </div>
        ))
      ) : (
        <MetricsPlaceholder hasMetrics={!!metrics} loading={metricsLoading} maintenanceSince={maintenanceSince} />
      )}
    </div>
  );
}
