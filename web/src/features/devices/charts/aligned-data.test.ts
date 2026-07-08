import { describe, it, expect } from 'vitest';
import { toFloat32, groupByFamily, buildFamilyChart, FAMILY_PALETTE } from './aligned-data';
import type { components } from '../../../types/api';

type MetricSeries = components['schemas']['MetricSeries'];

function series(overrides: Partial<MetricSeries> & { name: string; avg: (number | null)[] }): MetricSeries {
  return { min_max_source: 'none', ...overrides };
}

describe('toFloat32', () => {
  it('produces a Float32Array preserving numeric values', () => {
    const arr = toFloat32([10, 20, 30]);
    expect(arr).toBeInstanceOf(Float32Array);
    expect(Array.from(arr)).toEqual([10, 20, 30]);
  });

  it('maps null gaps to NaN (canvas skips NaN, so a gap renders)', () => {
    const arr = toFloat32([10, null, 30]);
    expect(arr[0]).toBe(10);
    expect(Number.isNaN(arr[1])).toBe(true);
    expect(arr[2]).toBe(30);
  });
});

describe('groupByFamily', () => {
  it('buckets series by the token before the first dot', () => {
    const groups = groupByFamily([
      series({ name: 'cpu.util', avg: [1] }),
      series({ name: 'cpu.load', avg: [1] }),
      series({ name: 'mem.used', avg: [1] }),
    ]);
    expect([...groups.keys()]).toEqual(['cpu', 'mem']);
    expect(groups.get('cpu')).toHaveLength(2);
    expect(groups.get('mem')).toHaveLength(1);
  });

  it('files a name with no dot under "other"', () => {
    const groups = groupByFamily([series({ name: 'uptime', avg: [1] })]);
    expect(groups.get('other')).toHaveLength(1);
  });
});

describe('buildFamilyChart', () => {
  it('builds a typed-array x axis (Float64) aligned 1:1 with the buckets', () => {
    const chart = buildFamilyChart([1000, 1010, 1020], [series({ name: 'cpu.util', avg: [10, 20, 30] })]);
    expect(chart.data[0]).toBeInstanceOf(Float64Array);
    expect(Array.from(chart.data[0]!)).toEqual([1000, 1010, 1020]);
  });

  it('renders only an avg line (no band) when min_max_source is none', () => {
    const chart = buildFamilyChart([1000, 1010], [series({ name: 'cpu.util', avg: [10, 20] })]);
    // data = [x, avg]
    expect(chart.data).toHaveLength(2);
    expect(chart.data[1]).toBeInstanceOf(Float32Array);
    expect(chart.bands).toHaveLength(0);
    // series[0] is the implicit x series; series[1] is the avg line
    expect(chart.series).toHaveLength(2);
    expect(chart.series[1]!.label).toBe('cpu.util');
  });

  it('adds min/max columns and a band when provenance is not none', () => {
    const chart = buildFamilyChart(
      [1000, 1010, 1020],
      [series({ name: 'cpu.util', avg: [10, null, 30], min: [5, null, 25], max: [15, null, 35], min_max_source: 'avg_of_10s' })],
    );
    // data = [x, avg, min, max]
    expect(chart.data).toHaveLength(4);
    expect(chart.data[1]).toBeInstanceOf(Float32Array);
    expect(chart.data[2]).toBeInstanceOf(Float32Array);
    expect(chart.data[3]).toBeInstanceOf(Float32Array);
    // avg gap preserved as NaN
    expect(Number.isNaN(chart.data[1]![1]!)).toBe(true);
    // one band filling between the max (idx 3) and min (idx 2) series
    expect(chart.bands).toHaveLength(1);
    expect(chart.bands[0]!.series).toEqual([3, 2]);
  });

  it('assigns a stable palette colour per metric across the family', () => {
    const chart = buildFamilyChart(
      [1000],
      [series({ name: 'net.rx', avg: [1] }), series({ name: 'net.tx', avg: [2] })],
    );
    expect(chart.series[1]!.stroke).toBe(FAMILY_PALETTE[0]);
    expect(chart.series[2]!.stroke).toBe(FAMILY_PALETTE[1]);
  });

  it('derives a finite y-scale range ignoring NaN gaps', () => {
    const chart = buildFamilyChart([1000, 1010], [series({ name: 'cpu.util', avg: [10, null] })]);
    expect(chart.scaleRange).not.toBeNull();
    const [lo, hi] = chart.scaleRange!;
    expect(Number.isFinite(lo)).toBe(true);
    expect(Number.isFinite(hi)).toBe(true);
    expect(lo).toBeLessThanOrEqual(10);
    expect(hi).toBeGreaterThanOrEqual(10);
  });
});
