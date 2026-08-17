/** One reading of an evidence series. */
export interface Reading {
  readonly ts: number;
  readonly value: number;
}

/** A series reduced to what it takes to draw and label it. */
export interface SparklineShape {
  /** SVG `points` for a polyline, in the caller's coordinate box. */
  polyline: string;
  min: number;
  max: number;
  /** The reading at the right-hand edge — where the alert fired. */
  last: number;
}

const round = (n: number) => Number(n.toFixed(2));

/**
 * Reduce a series to a polyline inside a `width` × `height` box.
 *
 * Evidence is frozen at write time and drawn here rather than fetched from a
 * charting engine, so the room stays a plain read of a snapshot: no library
 * loads, and nothing is asked of the machine that raised the alert.
 *
 * The x axis follows the clock rather than array position — an evidence window
 * is not evenly sampled, and spacing it evenly would move a spike away from when
 * it happened. A window whose readings all carry the same timestamp has no clock
 * to follow, so those fall back to even spacing.
 */
export function sparklineShape(
  readings: readonly Reading[],
  width: number,
  height: number,
): SparklineShape | null {
  if (readings.length < 2) return null;

  const values = readings.map((r) => r.value);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const valueRange = max - min;

  const first = readings.at(0);
  const final = readings.at(-1);
  if (!first || !final) return null;
  const tsRange = final.ts - first.ts;
  const lastIndex = readings.length - 1;

  const polyline = readings
    .map((reading, i) => {
      const x = tsRange === 0
        ? (i / lastIndex) * width
        : ((reading.ts - first.ts) / tsRange) * width;
      // A flat series has no range to scale against, so it sits down the middle
      // instead of collapsing onto an edge or dividing by zero.
      const y = valueRange === 0
        ? height / 2
        : height - ((reading.value - min) / valueRange) * height;
      return `${String(round(x))},${String(round(y))}`;
    })
    .join(' ');

  return { polyline, min, max, last: final.value };
}
