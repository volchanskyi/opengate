import { render, screen, fireEvent, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { DeviceMetrics } from './DeviceMetrics';

// Stub the imperative chart so these tests never touch uPlot/canvas; expose the
// drag-select callback via a button so the correlation drill-down is testable.
vi.mock('./charts/TimeSeriesChart', () => ({
  TimeSeriesChart: ({ ariaLabel, onSelectWindow, bands, yRange, height }: {
    ariaLabel?: string;
    onSelectWindow?: (f: number, t: number) => void;
    bands: readonly unknown[];
    yRange: readonly number[] | null;
    height: number;
  }) => (
    <div data-testid="chart" aria-label={ariaLabel} data-bands={String(bands.length)} data-range={JSON.stringify(yRange)} data-height={String(height)}>
      <button type="button" onClick={() => onSelectWindow?.(1_700_000_000, 1_700_003_600)}>select-{ariaLabel}</button>
    </div>
  ),
}));

const sampleMetrics = {
  t: [1_700_000_000, 1_700_000_060, 1_700_000_120],
  series: [
    { name: 'cpu.util', avg: [10, 20, 30], min: [5, 15, 25], max: [15, 25, 35], min_max_source: 'avg_of_10s' as const },
    { name: 'mem.used', avg: [40, 50, 60], min_max_source: 'avg_of_10s' as const },
  ],
  downsampled: true,
  bucket_s: 60,
};

const sampleCorrelation = {
  ranked: [
    { metric: 'cpu.util', score: 0.91, ks_statistic: 0.8, anomaly_rate: 0.5, shift_magnitude: 0.4, baseline_samples: 100, focus_samples: 50 },
  ],
  series_considered: 12,
  series_truncated: false,
};

function resetStore(overrides: Record<string, unknown> = {}) {
  useDeviceStore.setState({
    metrics: null,
    metricsLoading: false,
    correlation: null,
    correlationLoading: false,
    fetchMetrics: vi.fn().mockResolvedValue(undefined),
    correlate: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  });
}

describe('DeviceMetrics', () => {
  beforeEach(() => { resetStore(); });
  afterEach(() => { vi.useRealTimers(); });

  it('fetches the metrics window on mount with an avg_of_10s band', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-14T12:00:00Z'));
    const fetchMetrics = vi.fn().mockResolvedValue(undefined);
    resetStore({ fetchMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(fetchMetrics).toHaveBeenCalledWith('d1', {
      from: '2026-07-14T06:00:00.000Z',
      to: '2026-07-14T12:00:00.000Z',
      band: 'avg_of_10s',
      maxPoints: 1000,
    });
  });

  it('renders one timeline chart per metric family', () => {
    resetStore({ metrics: sampleMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    const charts = screen.getAllByTestId('chart');
    expect(charts).toHaveLength(2); // cpu + mem
    expect(screen.getByLabelText('cpu metrics')).toBeInTheDocument();
    expect(screen.getByLabelText('mem metrics')).toBeInTheDocument();
  });

  it('labels the band provenance honestly (avg_of_10s is not host extrema)', () => {
    resetStore({ metrics: sampleMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.getAllByText(/10 s averages/i).length).toBeGreaterThan(0);
  });

  it('labels local extrema and avg-only families exactly', () => {
    resetStore({
      metrics: {
        ...sampleMetrics,
        series: [
          { name: 'cpu.util', avg: [10], min: [5], max: [15], min_max_source: 'local' as const },
          { name: 'mem.used', avg: [20], min_max_source: 'none' as const },
        ],
      },
    });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.getByText('Band: host min/max (local history)')).toBeInTheDocument();
    expect(screen.getByText('avg only')).toBeInTheDocument();
    expect(screen.getByLabelText('cpu metrics')).toHaveAttribute('data-bands', '1');
    expect(screen.getByLabelText('mem metrics')).toHaveAttribute('data-bands', '0');
    expect(screen.getByLabelText('cpu metrics')).toHaveAttribute('data-height', '160');
  });

  it('shows the current anomaly rate in the anomaly panel', () => {
    resetStore({ metrics: sampleMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.getByText('50%')).toBeInTheDocument();
  });

  it('shows the latest utilisation reading beside each family header', () => {
    resetStore({
      metrics: {
        ...sampleMetrics,
        series: [
          { name: 'cpu.util', avg: [10, 20, 42], min_max_source: 'none' as const },
          { name: 'mem.used_percent', avg: [40, 50, 58], min_max_source: 'none' as const },
          { name: 'disk.used_percent', avg: [70, 71, 72], min_max_source: 'none' as const },
          { name: 'net.rx_bytes', avg: [0, 500_000, 1_000_000], min_max_source: 'none' as const },
          { name: 'net.tx_bytes', avg: [0, 250_000, 500_000], min_max_source: 'none' as const },
        ],
      },
    });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.1} />);
    expect(screen.getByText('42%')).toBeInTheDocument();
    expect(screen.getByText('58%')).toBeInTheDocument();
    expect(screen.getByText('72%')).toBeInTheDocument();
    expect(screen.getByText('1.4 MB')).toBeInTheDocument();
  });

  it('shows an empty-state message when the window has no telemetry', () => {
    resetStore({ metrics: { t: [], series: [], downsampled: false, bucket_s: 60 } });
    render(<DeviceMetrics deviceId="d1" anomalyRate={null} />);
    expect(screen.getByText(/no telemetry/i)).toBeInTheDocument();
  });

  it('renders the maintenance state in the edge-health panel instead of a health band', () => {
    resetStore({ metrics: sampleMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={undefined} maintenanceSince="2026-07-19T00:00:00Z" />);
    expect(screen.getByText('In maintenance')).toBeInTheDocument();
    // The "No data" health band must not stand in for the suppressed telemetry.
    expect(screen.queryByText('No data')).toBeNull();
  });

  it('shows a maintenance-aware empty state when telemetry is paused', () => {
    resetStore({ metrics: { t: [], series: [], downsampled: false, bucket_s: 60 } });
    render(<DeviceMetrics deviceId="d1" anomalyRate={null} maintenanceSince="2026-07-19T00:00:00Z" />);
    expect(screen.getByText(/in maintenance since/i)).toBeInTheDocument();
    expect(screen.queryByText(/no telemetry/i)).toBeNull();
  });

  it('re-fetches with a wider window when a preset is selected', async () => {
    const fetchMetrics = vi.fn().mockResolvedValue(undefined);
    resetStore({ fetchMetrics, metrics: sampleMetrics });
    const user = userEvent.setup();
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    fetchMetrics.mockClear();
    await user.click(screen.getByRole('button', { name: '24h' }));
    expect(fetchMetrics).toHaveBeenCalledTimes(1);
    const [, params] = fetchMetrics.mock.calls[0]!;
    const span = new Date(params.to).getTime() - new Date(params.from).getTime();
    expect(span).toBe(24 * 3600 * 1000);
    expect(screen.getByRole('button', { name: '24h' })).toHaveClass('bg-blue-600', 'text-white');
    expect(screen.getByRole('button', { name: '6h' })).toHaveClass('bg-gray-700', 'text-gray-300');
  });

  it('drag-selecting a window fires a debounced correlation for that window', () => {
    vi.useFakeTimers();
    const correlate = vi.fn().mockResolvedValue(undefined);
    resetStore({ correlate, metrics: sampleMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);

    fireEvent.click(screen.getByText('select-cpu metrics'));
    expect(correlate).not.toHaveBeenCalled(); // debounced
    act(() => { vi.advanceTimersByTime(399); });
    expect(correlate).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(1); });

    expect(correlate).toHaveBeenCalledWith('d1', {
      focusStart: new Date(1_700_000_000 * 1000).toISOString(),
      focusEnd: new Date(1_700_003_600 * 1000).toISOString(),
      topN: 8,
    });
  });

  it('renders the ranked correlation dimensions once results arrive', () => {
    resetStore({ metrics: sampleMetrics, correlation: sampleCorrelation });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.getByText(/top.*dimensions/i)).toBeInTheDocument();
    expect(screen.getByText('cpu.util')).toBeInTheDocument();
    expect(screen.getByText('0.91')).toBeInTheDocument();
    expect(screen.getByText('0.80')).toBeInTheDocument();
    expect(screen.getByText('0.50')).toBeInTheDocument();
  });

  it('excludes the log family (it has a dedicated sparkline beside the logs)', () => {
    resetStore({
      metrics: { ...sampleMetrics, series: [...sampleMetrics.series, { name: 'log.rate.self.error', avg: [1, 2, 3], min_max_source: 'none' as const }] },
    });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.getByLabelText('cpu metrics')).toBeInTheDocument();
    expect(screen.queryByLabelText('log metrics')).toBeNull();
  });

  it('jumps to logs for the current window via onViewLogs', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-14T12:00:00Z'));
    const onViewLogs = vi.fn();
    resetStore({ metrics: sampleMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} onViewLogs={onViewLogs} />);
    fireEvent.click(screen.getByText('View logs for this window'));
    expect(onViewLogs).toHaveBeenCalledWith(1_784_008_800, 1_784_030_400);
  });

  it('jumps to logs for a drag-selected window when one exists', () => {
    const onViewLogs = vi.fn();
    resetStore({ metrics: sampleMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} onViewLogs={onViewLogs} />);
    fireEvent.click(screen.getByText('select-cpu metrics'));
    fireEvent.click(screen.getByText('View logs for this window'));
    expect(onViewLogs).toHaveBeenCalledWith(1_700_000_000, 1_700_003_600);
  });

  it('hides the view-logs affordance when no handler is wired', () => {
    resetStore({ metrics: sampleMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.queryByText('View logs for this window')).toBeNull();
  });

  it('shows a loading state while the first metrics window is in flight', () => {
    resetStore({ metrics: null, metricsLoading: true });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.getByText(/loading metrics/i)).toBeInTheDocument();
  });

  it('shows a correlating state while a correlation is in flight', () => {
    resetStore({ metrics: sampleMetrics, correlationLoading: true });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.getByText(/correlating window/i)).toBeInTheDocument();
  });

  it('reports when no dimension broke pattern in the selected window', () => {
    resetStore({ metrics: sampleMetrics, correlation: { ranked: [], series_considered: 4, series_truncated: false } });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.getByText(/no dimension broke pattern/i)).toBeInTheDocument();
  });

  it('polls on schedule and clears the interval on unmount', () => {
    vi.useFakeTimers();
    const fetchMetrics = vi.fn().mockResolvedValue(undefined);
    resetStore({ fetchMetrics, metrics: sampleMetrics });
    const { unmount } = render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    fetchMetrics.mockClear();

    act(() => { vi.advanceTimersByTime(29_999); });
    expect(fetchMetrics).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(1); });
    expect(fetchMetrics).toHaveBeenCalledTimes(1);

    unmount();
    act(() => { vi.advanceTimersByTime(60_000); });
    expect(fetchMetrics).toHaveBeenCalledTimes(1);
  });

  it('cancels a pending correlation when unmounted', () => {
    vi.useFakeTimers();
    const correlate = vi.fn().mockResolvedValue(undefined);
    resetStore({ correlate, metrics: sampleMetrics });
    const { unmount } = render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    fireEvent.click(screen.getByText('select-cpu metrics'));
    unmount();
    act(() => { vi.advanceTimersByTime(400); });
    expect(correlate).not.toHaveBeenCalled();
  });

  it('rebuilds family charts when the store publishes new metrics', () => {
    resetStore({ metrics: sampleMetrics });
    render(<DeviceMetrics deviceId="d1" anomalyRate={0.5} />);
    expect(screen.getByLabelText('cpu metrics')).toBeInTheDocument();

    act(() => {
      useDeviceStore.setState({
        metrics: {
          ...sampleMetrics,
          series: [{ name: 'disk.used', avg: [1, 2, 3], min_max_source: 'none' as const }],
        },
      });
    });
    expect(screen.queryByLabelText('cpu metrics')).toBeNull();
    expect(screen.getByLabelText('disk metrics')).toBeInTheDocument();
  });
});
