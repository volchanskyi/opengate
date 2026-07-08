import { useLayoutEffect, useRef } from 'react';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';

export interface TimeSeriesChartProps {
  /** uPlot aligned data: `[xs, ...ys]`, xs unix seconds. Rebuilt by the caller, never React-rendered per point. */
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
  /** Fired with unix-second bounds when the user drag-selects a sub-window. */
  readonly onSelectWindow?: (fromSec: number, toSec: number) => void;
}

const DEFAULT_HEIGHT = 220;
const FALLBACK_WIDTH = 600;

function selectHook(onSelectWindow: (fromSec: number, toSec: number) => void) {
  return (self: uPlot): void => {
    const { left, width } = self.select;
    if (width <= 0) return;
    onSelectWindow(self.posToVal(left, 'x'), self.posToVal(left + width, 'x'));
  };
}

/**
 * Imperative uPlot wrapper — the only module that imports uPlot. React owns the
 * container (chrome); uPlot owns the canvas (pixels), fed typed arrays. The chart
 * is created once per series/band structure and updated in place via `setData`
 * on data ticks, so a polling refresh never re-runs the React reconciler over
 * thousands of points. The interface is engine-agnostic so a WebGL backend can
 * swap in behind it later.
 */
export function TimeSeriesChart({
  data,
  series,
  bands,
  yRange = null,
  height = DEFAULT_HEIGHT,
  className,
  ariaLabel,
  onSelectWindow,
}: TimeSeriesChartProps) {
  const containerRef = useRef<HTMLElement | null>(null);
  const chartRef = useRef<uPlot | null>(null);
  // Latest props read inside the mount effect without widening its deps — only a
  // structural change (series labels / band count) should rebuild the instance.
  // Synced in a layout effect (declared first, so it runs before the mount
  // effect each commit) rather than during render, which is forbidden for refs.
  const latest = useRef({ data, series, bands, yRange, height, onSelectWindow });
  useLayoutEffect(() => {
    latest.current = { data, series, bands, yRange, height, onSelectWindow };
  });

  const structureKey = `${series.map((s) => s.label ?? '').join('|')}#${String(bands?.length ?? 0)}`;

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const { data: d, series: s, bands: b, yRange: yr, height: h, onSelectWindow: onSel } = latest.current;
    const opts: uPlot.Options = {
      width: el.clientWidth || FALLBACK_WIDTH,
      height: h,
      series: [...s],
      cursor: { drag: { x: true, y: false } },
      ...(b && b.length > 0 ? { bands: [...b] } : {}),
      ...(yr ? { scales: { y: { range: [yr[0], yr[1]] } } } : {}),
      ...(onSel ? { hooks: { setSelect: [selectHook(onSel)] } } : {}),
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

  return <figure ref={containerRef} className={className} aria-label={ariaLabel} />;
}
