import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import type { components } from '../../types/api';
import { LogRateSparkline } from './LogRateSparkline';

type MetricRangeResponse = components['schemas']['MetricRangeResponse'];

// Capture what the sparkline hands the chart adapter without rendering uPlot.
const captured: { series: string[] } = { series: [] };
vi.mock('./charts/TimeSeriesChart', () => ({
  TimeSeriesChart: ({ ariaLabel, series }: { ariaLabel?: string; series: { label?: string }[] }) => {
    captured.series = series.map((s) => s.label ?? '');
    return <div data-testid="sparkline" aria-label={ariaLabel} />;
  },
}));

const withLogRate: MetricRangeResponse = {
  t: [1000, 1060],
  series: [
    { name: 'cpu.util', avg: [10, 20], min_max_source: 'none' },
    { name: 'log.rate.self.error', avg: [1, 3], min_max_source: 'none' },
    { name: 'log.rate.self.volume', avg: [40, 55], min_max_source: 'none' },
  ],
  downsampled: false,
  bucket_s: 60,
};

describe('LogRateSparkline', () => {
  it('renders a sparkline from only the log.* rate dimensions', () => {
    render(<LogRateSparkline metrics={withLogRate} />);
    expect(screen.getByTestId('sparkline')).toBeInTheDocument();
    // never plots cpu.util or raw messages — numeric log-rate dims only
    // (filter drops the implicit x series which carries no label)
    expect(captured.series.filter(Boolean)).toEqual(['log.rate.self.error', 'log.rate.self.volume']);
  });

  it('renders nothing when the window carries no log-rate dimensions', () => {
    const noLogs: MetricRangeResponse = {
      t: [1000], series: [{ name: 'cpu.util', avg: [10], min_max_source: 'none' }], downsampled: false, bucket_s: 60,
    };
    const { container } = render(<LogRateSparkline metrics={noLogs} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing when there are no metrics at all', () => {
    const { container } = render(<LogRateSparkline metrics={null} />);
    expect(container).toBeEmptyDOMElement();
  });
});
