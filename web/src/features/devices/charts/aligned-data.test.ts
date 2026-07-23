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
    const groups = groupByFamily([
      series({ name: 'uptime', avg: [1] }),
      series({ name: '.hidden', avg: [2] }),
      series({ name: 'cpu.', avg: [3] }),
    ]);
    expect(groups.get('other')?.map((s) => s.name)).toEqual(['uptime', '.hidden']);
    expect(groups.get('cpu')?.map((s) => s.name)).toEqual(['cpu.']);
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
    expect(chart.bands[0]!.fill).toBe(`${FAMILY_PALETTE[0]}22`);
    expect(chart.series[1]).toMatchObject({
      label: 'cpu.util', stroke: FAMILY_PALETTE[0], width: 1.5, scale: 'y', spanGaps: false,
    });
    expect(chart.series[2]).toMatchObject({
      label: 'cpu.util band', stroke: FAMILY_PALETTE[0], width: 0, scale: 'y', points: { show: false },
    });
    expect(chart.series[3]).toEqual(chart.series[2]);
  });

  it('assigns a stable palette colour per metric across the family', () => {
    const chart = buildFamilyChart(
      [1000],
      [series({ name: 'net.rx', avg: [1] }), series({ name: 'net.tx', avg: [2] })],
    );
    expect(chart.series[1]!.stroke).toBe(FAMILY_PALETTE[0]);
    expect(chart.series[2]!.stroke).toBe(FAMILY_PALETTE[1]);
  });

  it('wraps the palette after its final colour', () => {
    const metrics = Array.from({ length: FAMILY_PALETTE.length + 1 }, (_, i) =>
      series({ name: `net.metric${String(i)}`, avg: [i] }));
    const chart = buildFamilyChart([1000], metrics);
    expect(chart.series[1]!.stroke).toBe(FAMILY_PALETTE[0]);
    expect(chart.series[FAMILY_PALETTE.length + 1]!.stroke).toBe(FAMILY_PALETTE[0]);
  });

  it('does not draw a half band when either bound is absent', () => {
    const chart = buildFamilyChart([1000], [
      series({ name: 'cpu.util', avg: [10], min: [5], min_max_source: 'local' }),
      series({ name: 'cpu.load', avg: [20], max: [25], min_max_source: 'avg_of_10s' }),
    ]);
    expect(chart.data).toHaveLength(3);
    expect(chart.bands).toEqual([]);
    expect(chart.series).toHaveLength(3);
  });

  it('derives a finite y-scale range ignoring NaN gaps', () => {
    const chart = buildFamilyChart([1000, 1010, 1020, 1030], [
      series({ name: 'cpu.util', avg: [10, null, Number.NaN, Number.POSITIVE_INFINITY] }),
    ]);
    expect(chart.scaleRange).toEqual([9, 11]);
  });

  it('pads a varying finite range by exactly five percent', () => {
    const chart = buildFamilyChart([1000, 1010], [series({ name: 'cpu.util', avg: [10, 30] })]);
    expect(chart.scaleRange).toEqual([9, 31]);
  });

  it('uses magnitude padding for a flat nonzero range', () => {
    const chart = buildFamilyChart([1000], [series({ name: 'cpu.util', avg: [100] })]);
    expect(chart.scaleRange).toEqual([95, 105]);
  });

  it('returns no scale when every sample is a gap or non-finite', () => {
    const chart = buildFamilyChart([1000, 1010], [
      series({ name: 'cpu.util', avg: [null, Number.NEGATIVE_INFINITY] }),
    ]);
    expect(chart.scaleRange).toBeNull();
  });

  it('tracks the family maximum even when it is not the final sample', () => {
    // avg peaks at 30 mid-series then falls; the y-scale top must reflect the
    // running maximum (30), not merely the last value.
    const chart = buildFamilyChart([1000, 1010, 1020], [series({ name: 'cpu.util', avg: [10, 30, 20] })]);
    expect(chart.scaleRange).toEqual([9, 31]);
  });

  it('suppresses the band when provenance is none even if bounds are present', () => {
    const chart = buildFamilyChart([1000, 1010], [
      series({ name: 'cpu.util', avg: [10, 20], min: [5, 15], max: [15, 25], min_max_source: 'none' }),
    ]);
    // Provenance none means avg-only: no min/max columns, no band.
    expect(chart.data).toHaveLength(2);
    expect(chart.bands).toHaveLength(0);
    expect(chart.series).toHaveLength(2);
  });
});
