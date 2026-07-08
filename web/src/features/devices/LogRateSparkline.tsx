import { useMemo } from 'react';
import type { components } from '../../types/api';
import { TimeSeriesChart } from './charts/TimeSeriesChart';
import { buildFamilyChart } from './charts/aligned-data';

type MetricRangeResponse = components['schemas']['MetricRangeResponse'];

interface LogRateSparklineProps {
  readonly metrics: MetricRangeResponse | null;
  readonly onSelectWindow?: (fromSec: number, toSec: number) => void;
}

/**
 * Compact log-rate timeline over the same telemetry window, plotting only the
 * numeric `log.rate.*` dimensions (severity counts / volume). Raw message text
 * is never a chart label or tooltip — it lives solely in the explorer body.
 */
export function LogRateSparkline({ metrics, onSelectWindow }: LogRateSparklineProps) {
  const chart = useMemo(() => {
    if (!metrics) return null;
    const logSeries = metrics.series.filter((s) => s.name.startsWith('log.'));
    if (logSeries.length === 0) return null;
    return buildFamilyChart(metrics.t, logSeries);
  }, [metrics]);

  if (!chart) return null;

  return (
    <div className="mb-3">
      <p className="text-xs text-gray-400 mb-1">Log rate</p>
      <TimeSeriesChart
        data={chart.data}
        series={chart.series}
        bands={chart.bands}
        yRange={chart.scaleRange}
        height={60}
        ariaLabel="log rate"
        onSelectWindow={onSelectWindow}
      />
    </div>
  );
}
