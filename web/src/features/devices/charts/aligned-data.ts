import type uPlot from 'uplot';
import type { components } from '../../../types/api';

type MetricSeries = components['schemas']['MetricSeries'];

/**
 * Stroke colours for a family chart's lines, as canvas colour strings because uPlot
 * draws to a 2D context and Tailwind classes do not apply.
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
 * Converts a nullable column to a Float32Array with NaN gaps, which canvas `lineTo` skips.
 * `length` pads a short column with gaps and cuts a long one so column i stays aligned to x[i].
 */
export function toFloat32(values: readonly (number | null)[], length = values.length): Float32Array {
  // A fresh Float32Array is zero-filled and a zero is a reading, so the padding is NaN.
  const out = new Float32Array(length).fill(Number.NaN);
  out.set(Float32Array.from(values.slice(0, length), (v) => (typeof v === 'number' ? v : Number.NaN)));
  return out;
}

function familyOf(name: string): string {
  const dot = name.indexOf('.');
  return dot > 0 ? name.slice(0, dot) : 'other';
}

/** Buckets series by metric family (cpu.*, mem.*, …), keeping first-seen family order. */
export function groupByFamily(series: readonly MetricSeries[]): Map<string, MetricSeries[]> {
  const sites = new Map<string, MetricSeries[]>();
  for (const s of series) {
    const key = familyOf(s.name);
    const bucket = sites.get(key);
    if (bucket) bucket.push(s);
    else sites.set(key, [s]);
  }
  return sites;
}

/**
 * Widens [lo, hi] over a column's finite samples; the projected column is read, so readings
 * trimmed off the grid do not stretch the scale.
 */
function accumulateFinite(values: Float32Array, lo: number, hi: number): [number, number] {
  let nextLo = lo;
  let nextHi = hi;
  for (const v of values) {
    if (!Number.isFinite(v)) continue;
    if (v < nextLo) nextLo = v;
    if (v > nextHi) nextHi = v;
  }
  return [nextLo, nextHi];
}

/**
 * Builds the uPlot data, series and bands for one family: `[x, avg₀, (min₀, max₀)?, avg₁, …]`.
 * The y-scale range ignores NaN gaps.
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
    const avg = toFloat32(metric.avg, t.length);
    data.push(avg);
    series.push({ label: metric.name, stroke: color, width: 1.5, scale: 'y', spanGaps: false });
    [lo, hi] = accumulateFinite(avg, lo, hi);

    const { min, max } = metric;
    if (metric.min_max_source !== 'none' && min != null && max != null) {
      const minCol = toFloat32(min, t.length);
      const maxCol = toFloat32(max, t.length);
      data.push(minCol, maxCol);
      const minIdx = data.length - 2;
      const maxIdx = data.length - 1;
      // spanGaps stays off on the edges so a band never fills across a hole in the avg line.
      const faint = {
        label: `${metric.name} band`, stroke: color, width: 0, scale: 'y',
        spanGaps: false, points: { show: false },
      };
      series.push({ ...faint }, { ...faint });
      bands.push({ series: [maxIdx, minIdx], fill: `${color}22` });
      [lo, hi] = accumulateFinite(minCol, lo, hi);
      [lo, hi] = accumulateFinite(maxCol, lo, hi);
    }
  });

  let scaleRange: [number, number] | null = null;
  if (Number.isFinite(lo) && Number.isFinite(hi)) {
    const pad = hi > lo ? (hi - lo) * 0.05 : Math.max(Math.abs(hi) * 0.05, 1);
    scaleRange = [lo - pad, hi + pad];
  }

  return { data: data as uPlot.AlignedData, series, bands, scaleRange };
}

function lastFinite(values: readonly (number | null)[]): number | null {
  for (let i = values.length - 1; i >= 0; i--) {
    const v = values.at(i);
    if (typeof v === 'number' && Number.isFinite(v)) return v;
  }
  return null;
}

/** Formats bytes in binary units (e.g. `1.4 MB`); a count under 1 KB prints as plain bytes. */
function formatCompactBytes(bytes: number): string {
  if (bytes < 1024) return `${String(Math.round(bytes))} B`;
  const units = ['KB', 'MB', 'GB', 'TB', 'PB'];
  let val = bytes / 1024;
  let idx = 0;
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024;
    idx += 1;
  }
  return `${val.toFixed(val >= 100 ? 0 : 1)} ${units.at(idx) ?? 'PB'}`;
}

function isPercentDimension(name: string): boolean {
  return /_percent$/.test(name) || name === 'cpu.util' || name === 'cpu.total';
}

/**
 * Current reading shown beside a family chart: the latest utilisation percent, or rx + tx
 * bytes/second for net; null when the family has no such dimension or no finite sample.
 */
export function familyCurrentLabel(series: readonly MetricSeries[]): string | null {
  const percent = series.find((s) => isPercentDimension(s.name));
  if (percent) {
    const v = lastFinite(percent.avg);
    return v === null ? null : `${String(Math.round(v))}%`;
  }
  if (series.some((s) => s.name.startsWith('net.'))) {
    let total = 0;
    let any = false;
    for (const s of series) {
      const v = lastFinite(s.avg);
      if (v !== null) {
        total += v;
        any = true;
      }
    }
    return any ? `${formatCompactBytes(total)}/s` : null;
  }
  return null;
}
