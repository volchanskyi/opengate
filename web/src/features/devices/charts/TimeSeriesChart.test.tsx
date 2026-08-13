import { render, cleanup } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type uPlot from 'uplot';
import { TimeSeriesChart } from './TimeSeriesChart';

interface FakeInstance {
  opts: uPlot.Options;
  data: uPlot.AlignedData;
  target: HTMLElement | undefined;
  setData: ReturnType<typeof vi.fn>;
  setScale: ReturnType<typeof vi.fn>;
  setSize: ReturnType<typeof vi.fn>;
  destroy: ReturnType<typeof vi.fn>;
}

const mock = vi.hoisted(() => {
  const instances: FakeInstance[] = [];
  // Regular function (not arrow) so `new uPlot(...)` in the adapter can construct it.
  const ctor = vi.fn(function FakeUplot(opts: uPlot.Options, data: uPlot.AlignedData, target?: HTMLElement) {
    const inst: FakeInstance = {
      opts,
      data,
      target,
      setData: vi.fn(),
      setScale: vi.fn(),
      setSize: vi.fn(),
      destroy: vi.fn(),
    };
    instances.push(inst);
    return inst;
  });
  return { instances, ctor };
});

vi.mock('uplot', () => ({ default: mock.ctor }));

function makeData(ys: number[]): uPlot.AlignedData {
  return [Float64Array.from([1000, 1010, 1020]), Float32Array.from(ys)];
}

const series: uPlot.Series[] = [{}, { label: 'cpu.util', stroke: '#60a5fa' }];

afterEach(() => {
  cleanup();
  mock.instances.length = 0;
  mock.ctor.mockClear();
});

describe('TimeSeriesChart adapter', () => {
  it('constructs one uPlot instance with the passed series and typed-array data', () => {
    render(<TimeSeriesChart data={makeData([1, 2, 3])} series={series} />);
    expect(mock.ctor).toHaveBeenCalledTimes(1);
    const inst = mock.instances[0]!;
    expect(inst.opts.series).toEqual(series);
    // x axis stays a Float64Array — the render path never touches React state.
    expect(inst.data[0]).toBeInstanceOf(Float64Array);
    expect(inst.data[1]).toBeInstanceOf(Float32Array);
  });

  it('mounts into a real DOM element (canvas owns pixels, React owns chrome)', () => {
    render(<TimeSeriesChart data={makeData([1, 2, 3])} series={series} ariaLabel="cpu chart" />);
    expect(mock.ctor.mock.calls[0]![2]).toBeInstanceOf(HTMLElement);
  });

  it('calls setSize from the ResizeObserver so the canvas tracks its container', () => {
    render(<TimeSeriesChart data={makeData([1, 2, 3])} series={series} />);
    expect(mock.instances[0]!.setSize).toHaveBeenCalled();
  });

  it('pushes new data through setData without reconstructing the chart', () => {
    const { rerender } = render(<TimeSeriesChart data={makeData([1, 2, 3])} series={series} />);
    const next = makeData([9, 8, 7]);
    rerender(<TimeSeriesChart data={next} series={series} />);
    expect(mock.ctor).toHaveBeenCalledTimes(1); // no reconstruction
    expect(mock.instances[0]!.setData).toHaveBeenLastCalledWith(next);
  });

  it('destroys the uPlot instance on unmount', () => {
    const { unmount } = render(<TimeSeriesChart data={makeData([1, 2, 3])} series={series} />);
    const inst = mock.instances[0]!;
    unmount();
    expect(inst.destroy).toHaveBeenCalledTimes(1);
  });

  it('applies an explicit finite y-scale range when provided', () => {
    render(<TimeSeriesChart data={makeData([1, 2, 3])} series={series} yRange={[0, 100]} />);
    expect(mock.instances[0]!.opts.scales?.y?.range).toEqual([0, 100]);
  });

  it('disables the default uPlot legend (removes the "Time --" row)', () => {
    render(<TimeSeriesChart data={makeData([1, 2, 3])} series={series} />);
    expect(mock.instances[0]!.opts.legend?.show).toBe(false);
  });

  it('leaves the cursor read-only: a drag neither zooms nor selects', () => {
    render(<TimeSeriesChart data={makeData([1, 2, 3])} series={series} />);
    const { opts } = mock.instances[0]!;
    expect(opts.cursor?.drag).toEqual({ x: false, y: false });
    expect(opts.hooks?.setSelect).toBeUndefined();
  });

  it('re-applies the y-scale via setScale when a poll brings a wider yRange (clip regression)', () => {
    const { rerender } = render(<TimeSeriesChart data={makeData([1, 2, 3])} series={series} yRange={[0, 10]} />);
    const inst = mock.instances[0]!;
    // A later poll brings a new peak (50) beyond the initial window → the caller
    // passes a wider yRange. Without setScale the stale fixed range clips the line.
    rerender(<TimeSeriesChart data={makeData([1, 2, 50])} series={series} yRange={[0, 55]} />);
    expect(mock.ctor).toHaveBeenCalledTimes(1); // no reconstruction
    expect(inst.setScale).toHaveBeenLastCalledWith('y', { min: 0, max: 55 });
  });
});
