import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useDeviceStore } from './device-store';

const mockGet = vi.fn();
const mockPost = vi.fn();

vi.mock('../../../lib/api', () => ({
  api: {
    GET: (...args: unknown[]) => mockGet(...args),
    POST: (...args: unknown[]) => mockPost(...args),
  },
}));

const sampleMetrics = {
  t: [1000, 1060, 1120],
  series: [{ name: 'cpu.util', avg: [10, 20, 30], min_max_source: 'avg_of_10s' as const }],
  downsampled: true,
  bucket_s: 60,
};

const sampleCorrelation = {
  ranked: [
    { metric: 'cpu.util', score: 0.9, ks_statistic: 0.8, anomaly_rate: 0.5, shift_magnitude: 0.4, baseline_samples: 100, focus_samples: 50 },
  ],
  series_considered: 12,
  series_truncated: false,
};

describe('device store — telemetry', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      metrics: null,
      metricsLoading: false,
      correlation: null,
      correlationLoading: false,
      isLoading: false,
      error: null,
    });
  });

  it('fetchMetrics stores the window and clears loading on success', async () => {
    mockGet.mockResolvedValue({ data: sampleMetrics, response: { ok: true } });
    await useDeviceStore.getState().fetchMetrics('d1', { from: '2026-01-01T00:00:00Z', to: '2026-01-01T01:00:00Z' });

    expect(mockGet).toHaveBeenCalledWith('/api/v1/devices/{id}/metrics', expect.objectContaining({
      params: expect.objectContaining({
        path: { id: 'd1' },
        query: expect.objectContaining({ from: '2026-01-01T00:00:00Z', to: '2026-01-01T01:00:00Z' }),
      }),
    }));
    expect(useDeviceStore.getState().metrics).toEqual(sampleMetrics);
    expect(useDeviceStore.getState().metricsLoading).toBe(false);
  });

  it('fetchMetrics forwards max_points and band when provided', async () => {
    mockGet.mockResolvedValue({ data: sampleMetrics, response: { ok: true } });
    await useDeviceStore.getState().fetchMetrics('d1', {
      from: 'a', to: 'b', maxPoints: 500, band: 'none',
    });
    const [, opts] = mockGet.mock.calls[0]!;
    expect(opts.params.query.max_points).toBe(500);
    expect(opts.params.query.band).toBe('none');
  });

  it('fetchMetrics includes dimensions only when the list is non-empty', async () => {
    mockGet.mockResolvedValue({ data: sampleMetrics, response: { ok: true } });

    await useDeviceStore.getState().fetchMetrics('d1', { from: 'a', to: 'b', dims: [] });
    const omittedQuery = mockGet.mock.calls[0]![1].params.query;
    expect(omittedQuery).toEqual({ from: 'a', to: 'b' });
    expect(Object.hasOwn(omittedQuery, 'dims')).toBe(false);
    expect(Object.hasOwn(omittedQuery, 'max_points')).toBe(false);
    expect(Object.hasOwn(omittedQuery, 'band')).toBe(false);

    await useDeviceStore.getState().fetchMetrics('d1', {
      from: 'c', to: 'd', dims: ['cpu.util', 'memory.used'], maxPoints: 0, band: 'none',
    });
    expect(mockGet.mock.calls[1]![1].params.query).toEqual({
      from: 'c',
      to: 'd',
      dims: ['cpu.util', 'memory.used'],
      max_points: 0,
      band: 'none',
    });
  });

  it('fetchMetrics raises its dedicated loading flag before the request settles', async () => {
    let resolve!: (value: unknown) => void;
    mockGet.mockReturnValueOnce(new Promise((r) => { resolve = r; }));

    const pending = useDeviceStore.getState().fetchMetrics('d1', { from: 'a', to: 'b' });
    expect(useDeviceStore.getState().metricsLoading).toBe(true);

    resolve({ data: sampleMetrics, response: { ok: true } });
    await pending;
    expect(useDeviceStore.getState().metricsLoading).toBe(false);
  });

  it('fetchMetrics never toggles the global isLoading spinner', async () => {
    mockGet.mockResolvedValue({ data: sampleMetrics, response: { ok: true } });
    let peak = false;
    const unsub = useDeviceStore.subscribe((s) => { if (s.isLoading) peak = true; });
    await useDeviceStore.getState().fetchMetrics('d1', { from: 'a', to: 'b' });
    unsub();
    expect(peak).toBe(false);
    // uses the dedicated metricsLoading flag instead
  });

  it('fetchMetrics leaves metrics null and clears loading on failure', async () => {
    mockGet.mockResolvedValue({ error: { error: 'unavailable' }, response: { ok: false, status: 503 } });
    await useDeviceStore.getState().fetchMetrics('d1', { from: 'a', to: 'b' });
    expect(useDeviceStore.getState().metrics).toBeNull();
    expect(useDeviceStore.getState().metricsLoading).toBe(false);
  });

  it('correlate posts the focus window and stores the ranked result', async () => {
    mockPost.mockResolvedValue({ data: sampleCorrelation, response: { ok: true } });
    await useDeviceStore.getState().correlate('d1', { focusStart: 'f0', focusEnd: 'f1' });

    expect(mockPost).toHaveBeenCalledWith('/api/v1/devices/{id}/correlate', expect.objectContaining({
      params: { path: { id: 'd1' } },
      body: expect.objectContaining({ focus_start: 'f0', focus_end: 'f1' }),
    }));
    expect(useDeviceStore.getState().correlation).toEqual(sampleCorrelation);
    expect(useDeviceStore.getState().correlationLoading).toBe(false);
  });

  it('correlate forwards the optional baseline window and top_n', async () => {
    mockPost.mockResolvedValue({ data: sampleCorrelation, response: { ok: true } });
    await useDeviceStore.getState().correlate('d1', {
      focusStart: 'f0', focusEnd: 'f1', baselineStart: 'b0', baselineEnd: 'b1', topN: 5,
    });
    const [, opts] = mockPost.mock.calls[0]!;
    expect(opts.body).toMatchObject({ baseline_start: 'b0', baseline_end: 'b1', top_n: 5 });
  });

  it('correlate omits absent options and preserves a zero top_n', async () => {
    mockPost.mockResolvedValue({ data: sampleCorrelation, response: { ok: true } });

    await useDeviceStore.getState().correlate('d1', { focusStart: 'f0', focusEnd: 'f1' });
    const omittedBody = mockPost.mock.calls[0]![1].body;
    expect(omittedBody).toEqual({ focus_start: 'f0', focus_end: 'f1' });
    expect(Object.hasOwn(omittedBody, 'baseline_start')).toBe(false);
    expect(Object.hasOwn(omittedBody, 'baseline_end')).toBe(false);
    expect(Object.hasOwn(omittedBody, 'top_n')).toBe(false);

    await useDeviceStore.getState().correlate('d1', { focusStart: 'g0', focusEnd: 'g1', topN: 0 });
    expect(mockPost.mock.calls[1]![1].body).toEqual({ focus_start: 'g0', focus_end: 'g1', top_n: 0 });
  });

  it('correlate raises its dedicated loading flag before the request settles', async () => {
    let resolve!: (value: unknown) => void;
    mockPost.mockReturnValueOnce(new Promise((r) => { resolve = r; }));

    const pending = useDeviceStore.getState().correlate('d1', { focusStart: 'f0', focusEnd: 'f1' });
    expect(useDeviceStore.getState().correlationLoading).toBe(true);

    resolve({ data: sampleCorrelation, response: { ok: true } });
    await pending;
    expect(useDeviceStore.getState().correlationLoading).toBe(false);
  });

  it('correlate never toggles the global loading spinner', async () => {
    mockPost.mockResolvedValue({ data: sampleCorrelation, response: { ok: true } });
    let peak = false;
    const unsubscribe = useDeviceStore.subscribe((state) => { if (state.isLoading) peak = true; });
    await useDeviceStore.getState().correlate('d1', { focusStart: 'f0', focusEnd: 'f1' });
    unsubscribe();
    expect(peak).toBe(false);
  });

  it('correlate leaves correlation null and clears loading on failure', async () => {
    mockPost.mockResolvedValue({ error: { error: 'at capacity' }, response: { ok: false, status: 503 } });
    await useDeviceStore.getState().correlate('d1', { focusStart: 'f0', focusEnd: 'f1' });
    expect(useDeviceStore.getState().correlation).toBeNull();
    expect(useDeviceStore.getState().correlationLoading).toBe(false);
  });
});
