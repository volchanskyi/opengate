import { useLayoutEffect, useRef } from 'react';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';

export interface TimeSeriesChartProps {
  /** uPlot aligned data: `[xs, ...ys]`, xs unix seconds. */
  readonly data: uPlot.AlignedData;
  /** uPlot series config, index 0 is the implicit x series. */
  readonly series: readonly uPlot.Series[];
  /** Optional fill bands (e.g. min–max), referencing series indices. */
  readonly bands?: readonly uPlot.Band[];
  /** Explicit finite y-scale range; when null uPlot auto-ranges. */
  readonly yRange?: readonly [number, number] | null;
  readonly height?: number;
  readonly className?: string;
  readonly ariaLabel?: string;
}

const DEFAULT_HEIGHT = 220;
const FALLBACK_WIDTH = 600;

/**
 * TimeSeriesChart wraps uPlot imperatively: it rebuilds only when the series or band structure
 * changes and pushes new data in place through `setData`.
 */
export function TimeSeriesChart({
  data,
  series,
  bands,
  yRange = null,
  height = DEFAULT_HEIGHT,
  className,
  ariaLabel,
}: TimeSeriesChartProps) {
  const containerRef = useRef<HTMLElement | null>(null);
  const chartRef = useRef<uPlot | null>(null);
  // The mount effect reads the latest props from this ref, so only a structure change rebuilds.
  // A layout effect declared first syncs it each commit, since refs are not written in render.
  const latest = useRef({ data, series, bands, yRange, height });
  useLayoutEffect(() => {
    latest.current = { data, series, bands, yRange, height };
  });

  const structureKey = `${series.map((s) => s.label ?? '').join('|')}#${String(bands?.length ?? 0)}`;

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const { data: d, series: s, bands: b, yRange: yr, height: h } = latest.current;
    const opts: uPlot.Options = {
      width: el.clientWidth || FALLBACK_WIDTH,
      height: h,
      series: [...s],
      // The current value shows beside each family title, so uPlot's default legend is off.
      legend: { show: false },
      // The cursor only reads values, since a zoom drag would be undone by the next poll's setData.
      cursor: { drag: { x: false, y: false } },
      ...(b && b.length > 0 ? { bands: [...b] } : {}),
      ...(yr ? { scales: { y: { range: [yr[0], yr[1]] } } } : {}),
    };
    const chart = new uPlot(opts, d, el);
    chartRef.current = chart;
    const observer = new ResizeObserver(() => {
      chart.setSize({ width: el.clientWidth || FALLBACK_WIDTH, height: latest.current.height });
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
      chart.destroy();
      chartRef.current = null;
    };
  }, [structureKey]);

  useLayoutEffect(() => {
    chartRef.current?.setData(data);
  }, [data]);

  // Re-applies the y-scale on every yRange change, after setData; the mount effect sets it once.
  const yMin = yRange?.[0];
  const yMax = yRange?.[1];
  useLayoutEffect(() => {
    if (yMin == null || yMax == null) return;
    chartRef.current?.setScale('y', { min: yMin, max: yMax });
  }, [yMin, yMax]);

  return <figure ref={containerRef} className={className} aria-label={ariaLabel} />;
}
