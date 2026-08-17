import { describe, it, expect } from 'vitest';
import { sparklineShape } from './sparkline';

const at = (ts: number, value: number) => ({ ts, value });

describe('sparklineShape', () => {
  it('draws nothing from fewer than two readings — one point is not a shape', () => {
    expect(sparklineShape([], 100, 20)).toBeNull();
    expect(sparklineShape([at(0, 5)], 100, 20)).toBeNull();
  });

  it('spans the full width and inverts y, because SVG counts downwards', () => {
    const shape = sparklineShape([at(0, 0), at(10, 1)], 20, 10);
    expect(shape?.polyline).toBe('0,10 20,0');
  });

  it('places a reading by its timestamp, not by its position in the array', () => {
    // The middle reading lands a quarter along, where its clock says it happened.
    const shape = sparklineShape([at(0, 0), at(25, 0), at(100, 0)], 100, 10);
    expect(shape?.polyline).toBe('0,5 25,5 100,5');
  });

  it('spaces readings evenly when every timestamp is the same', () => {
    const shape = sparklineShape([at(7, 0), at(7, 1), at(7, 0)], 100, 10);
    expect(shape?.polyline).toBe('0,10 50,0 100,10');
  });

  it('draws a flat series down the middle rather than dividing by a zero range', () => {
    const shape = sparklineShape([at(0, 42), at(10, 42)], 20, 10);
    expect(shape?.polyline).toBe('0,5 20,5');
  });

  it('reports the extremes and the last reading the caller labels the chart with', () => {
    const shape = sparklineShape([at(0, 3), at(1, 9), at(2, 1), at(3, 4)], 100, 20);
    expect(shape?.min).toBe(1);
    expect(shape?.max).toBe(9);
    expect(shape?.last).toBe(4);
  });

  it('rounds coordinates so the path stays small', () => {
    const shape = sparklineShape([at(0, 0), at(3, 1), at(7, 2)], 10, 10);
    expect(shape?.polyline).toBe('0,10 4.29,5 10,0');
  });
});
