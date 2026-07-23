import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { components } from '../../types/api';
import { useDeviceStore } from './state/device-store';
import { TimeSeriesChart } from './charts/TimeSeriesChart';
import { buildFamilyChart, groupByFamily } from './charts/aligned-data';
import { HealthBadge } from './HealthBadge';
import { healthBand, HEALTH_META } from './health';
import { formatMaintenanceSince } from './maintenance';
import { fireAndForget } from '../../lib/fire-and-forget';

type MinMaxSource = components['schemas']['MetricSeries']['min_max_source'];
type CorrelateResponse = components['schemas']['CorrelateResponse'];

const MAX_POINTS = 1000;
const CORRELATE_DEBOUNCE_MS = 400;
const POLL_MS = 30_000;
const CORRELATE_TOP_N = 8;

const PRESETS = [
  { key: '1h', seconds: 3600 },
  { key: '6h', seconds: 6 * 3600 },
  { key: '24h', seconds: 24 * 3600 },
  { key: '7d', seconds: 7 * 24 * 3600 },
] as const;

const DEFAULT_PRESET = '6h';

/** Honest description of what a family's band represents (central VM is avg-only). */
function bandCaption(hasBand: boolean, source: MinMaxSource): string {
  if (!hasBand) return 'avg only';
  if (source === 'local') return 'Band: host min/max (local history)';
  if (source === 'avg_of_10s') return 'Band: min/max across 10 s averages (not host extrema)';
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

function CorrelationTable({ result, loading }: { readonly result: CorrelateResponse | null; readonly loading: boolean }) {
  if (loading) return <p className="text-xs text-gray-400">Correlating window…</p>;
  if (!result) return null;
  if (result.ranked.length === 0) {
    return <p className="text-xs text-gray-500">No dimension broke pattern in the selected window.</p>;
  }
  return (
    <div>
      <h4 className="text-xs font-semibold text-gray-300 mb-1">Top anomalous dimensions</h4>
      <table className="w-full text-xs font-mono">
        <thead>
          <tr className="text-left text-gray-500">
            <th className="pr-2">Metric</th><th className="pr-2">Score</th><th className="pr-2">KS</th><th>Anom.</th>
          </tr>
        </thead>
        <tbody>
          {result.ranked.map((d) => (
            <tr key={d.metric} className="text-gray-300">
              <td className="pr-2">{d.metric}</td>
              <td className="pr-2">{d.score.toFixed(2)}</td>
              <td className="pr-2">{d.ks_statistic.toFixed(2)}</td>
              <td>{d.anomaly_rate.toFixed(2)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

interface DeviceMetricsProps {
  readonly deviceId: string;
  readonly anomalyRate?: number | null;
  /** When set, the device is in maintenance: telemetry is paused, so the panel
   *  shows the since-when state rather than a stale health band or "no data". */
  readonly maintenanceSince?: string | null;
  /** Correlation jump: open the logs explorer for a window (unix seconds). */
  readonly onViewLogs?: (fromSec: number, toSec: number) => void;
}

/**
 * Device-detail telemetry panel: an anomaly summary, per-family metric timelines
 * (avg line + honest provenance band), and a Netdata-style correlation
 * drill-down — drag a window on any chart to rank the dimensions that broke
 * pattern. All heavy rendering is delegated to the imperative chart adapter.
 */
export function DeviceMetrics({ deviceId, anomalyRate, maintenanceSince, onViewLogs }: DeviceMetricsProps) {
  const metrics = useDeviceStore((s) => s.metrics);
  const metricsLoading = useDeviceStore((s) => s.metricsLoading);
  const fetchMetrics = useDeviceStore((s) => s.fetchMetrics);
  const correlation = useDeviceStore((s) => s.correlation);
  const correlationLoading = useDeviceStore((s) => s.correlationLoading);
  const correlate = useDeviceStore((s) => s.correlate);

  const [presetKey, setPresetKey] = useState<string>(DEFAULT_PRESET);
  const [selectedWindow, setSelectedWindow] = useState<{ fromSec: number; toSec: number } | null>(null);
  const seconds = PRESETS.find((p) => p.key === presetKey)?.seconds ?? PRESETS[1].seconds;
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const load = useCallback(() => {
    const to = new Date();
    const from = new Date(to.getTime() - seconds * 1000);
    fireAndForget(fetchMetrics(deviceId, {
      from: from.toISOString(),
      to: to.toISOString(),
      band: 'avg_of_10s',
      maxPoints: MAX_POINTS,
    }));
  }, [deviceId, seconds, fetchMetrics]);

  useEffect(() => { load(); }, [load]);

  // Keep the window fresh without re-running the React reconciler over points —
  // the adapter pushes new data through setData.
  useEffect(() => {
    const id = setInterval(load, POLL_MS);
    return () => { clearInterval(id); };
  }, [load]);

  useEffect(() => () => { if (debounceRef.current) clearTimeout(debounceRef.current); }, []);

  const handleSelectWindow = useCallback((fromSec: number, toSec: number) => {
    setSelectedWindow({ fromSec, toSec });
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      fireAndForget(correlate(deviceId, {
        focusStart: new Date(fromSec * 1000).toISOString(),
        focusEnd: new Date(toSec * 1000).toISOString(),
        topN: CORRELATE_TOP_N,
      }));
    }, CORRELATE_DEBOUNCE_MS);
  }, [deviceId, correlate]);

  const handleViewLogs = useCallback(() => {
    if (!onViewLogs) return;
    if (selectedWindow) {
      onViewLogs(selectedWindow.fromSec, selectedWindow.toSec);
      return;
    }
    const toSec = Math.floor(Date.now() / 1000);
    onViewLogs(toSec - seconds, toSec);
  }, [onViewLogs, selectedWindow, seconds]);

  // System-metric families exclude the `log` family: its dimensions are log
  // volume/severity counts, not a system resource, so they are not charted here.
  const families = useMemo(() => {
    if (!metrics) return [];
    return [...groupByFamily(metrics.series).entries()]
      .filter(([family]) => family !== 'log')
      .map(([family, series]) => {
        const chart = buildFamilyChart(metrics.t, series);
        return { family, chart, source: series[0]?.min_max_source ?? 'none' as MinMaxSource };
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
              className="px-2 py-1 rounded text-xs bg-gray-700 text-gray-200 hover:bg-gray-600"
            >
              View logs for this window
            </button>
          )}
          <div className="flex gap-1" role="group" aria-label="Metrics window">
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
        <>
          <p className="text-xs text-gray-500">Drag across a chart to correlate that window.</p>
          {families.map(({ family, chart, source }) => (
            <div key={family}>
              <div className="flex items-center justify-between">
                <h4 className="text-xs font-semibold text-gray-300 capitalize">{family}</h4>
                <span className="text-[10px] text-gray-500">{bandCaption(chart.bands.length > 0, source)}</span>
              </div>
              <TimeSeriesChart
                data={chart.data}
                series={chart.series}
                bands={chart.bands}
                yRange={chart.scaleRange}
                height={160}
                ariaLabel={`${family} metrics`}
                onSelectWindow={handleSelectWindow}
              />
            </div>
          ))}
          <CorrelationTable result={correlation} loading={correlationLoading} />
        </>
      ) : (
        <MetricsPlaceholder hasMetrics={!!metrics} loading={metricsLoading} maintenanceSince={maintenanceSince} />
      )}
    </div>
  );
}
