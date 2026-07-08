import type uPlot from 'uplot';
import type { components } from '../../../types/api';

type MetricSeries = components['schemas']['MetricSeries'];

/**
 * Stable per-metric stroke colours (Tailwind-derived hex) used across a family
 * chart's lines. Kept as canvas colour strings — uPlot draws to a 2D context,
 * not the DOM, so Tailwind classes do not apply here.
 */
export const FAMILY_PALETTE = [
  '#60a5fa', // blue-400
  '#f472b6', // pink-400
  '#34d399', // emerald-400
  '#fbbf24', // amber-400
  '#a78bfa', // violet-400
  '#f87171', // red-400
  '#22d3ee', // cyan-400
  '#a3e635', // lime-400
] as const;

/** uPlot inputs for one metric-family chart, ready to hand to the adapter. */
export interface FamilyChart {
  data: uPlot.AlignedData;
  series: uPlot.Series[];
  bands: uPlot.Band[];
  /** Finite y-scale range, or null when the family has no finite samples. */
  scaleRange: [number, number] | null;
}

/**
 * Convert a nullable numeric column into a Float32Array, mapping null gaps to
 * NaN. Canvas `lineTo(x, NaN)` is a no-op, so a NaN renders as a break in the
 * line rather than a spurious zero — the typed array keeps the render path off
 * the React reconciler.
 */
export function toFloat32(values: readonly (number | null)[]): Float32Array {
  return Float32Array.from(values, (v) => (typeof v === 'number' ? v : Number.NaN));
}

/** Family key: the token before the first dot, or "other" for un-dotted names. */
function familyOf(name: string): string {
  const dot = name.indexOf('.');
  return dot > 0 ? name.slice(0, dot) : 'other';
}

/**
 * Bucket numeric series by metric family (cpu.*, mem.*, disk.*, net.*, …) so
 * the ~100-dimension device firehose decomposes into a handful of readable
 * charts. Insertion order of first-seen families is preserved.
 */
export function groupByFamily(series: readonly MetricSeries[]): Map<string, MetricSeries[]> {
  const groups = new Map<string, MetricSeries[]>();
  for (const s of series) {
    const key = familyOf(s.name);
    const bucket = groups.get(key);
    if (bucket) bucket.push(s);
    else groups.set(key, [s]);
  }
  return groups;
}

function accumulateFinite(values: readonly (number | null)[], lo: number, hi: number): [number, number] {
  let nextLo = lo;
  let nextHi = hi;
  for (const v of values) {
    if (v === null || !Number.isFinite(v)) continue;
    if (v < nextLo) nextLo = v;
    if (v > nextHi) nextHi = v;
  }
  return [nextLo, nextHi];
}

/**
 * Build the uPlot aligned-data + series + band configuration for one family.
 *
 * Layout: `data = [x, avg₀, (min₀, max₀)?, avg₁, …]`. Each metric contributes an
 * avg line always; when it carries a non-`none` band it also contributes faint
 * min/max edge series and a fill band between them. The y-scale range is
 * computed here (ignoring NaN gaps) so a gap never poisons the auto-range.
 */
export function buildFamilyChart(
  t: readonly number[],
  metrics: readonly MetricSeries[],
  palette: readonly string[] = FAMILY_PALETTE,
): FamilyChart {
  const data: (Float64Array | Float32Array)[] = [Float64Array.from(t)];
  const series: uPlot.Series[] = [{}];
  const bands: uPlot.Band[] = [];
  let lo = Infinity;
  let hi = -Infinity;

  metrics.forEach((metric, i) => {
    const color = palette[i % palette.length];
    data.push(toFloat32(metric.avg));
    series.push({ label: metric.name, stroke: color, width: 1.5, scale: 'y', spanGaps: false });
    [lo, hi] = accumulateFinite(metric.avg, lo, hi);

    const { min, max } = metric;
    if (metric.min_max_source !== 'none' && min != null && max != null) {
      data.push(toFloat32(min), toFloat32(max));
      const minIdx = data.length - 2;
      const maxIdx = data.length - 1;
      const faint = { label: `${metric.name} band`, stroke: color, width: 0, scale: 'y', points: { show: false } };
      series.push({ ...faint }, { ...faint });
      bands.push({ series: [maxIdx, minIdx], fill: `${color}22` });
      [lo, hi] = accumulateFinite(min, lo, hi);
      [lo, hi] = accumulateFinite(max, lo, hi);
    }
  });

  let scaleRange: [number, number] | null = null;
  if (Number.isFinite(lo) && Number.isFinite(hi)) {
    const pad = hi > lo ? (hi - lo) * 0.05 : Math.max(Math.abs(hi) * 0.05, 1);
    scaleRange = [lo - pad, hi + pad];
  }

  return { data: data as uPlot.AlignedData, series, bands, scaleRange };
}
