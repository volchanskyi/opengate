import { type Reading, sparklineShape } from './sparkline';

const WIDTH = 220;
const HEIGHT = 40;

/** Trim a reading to the precision anybody acts on. */
const trim = (v: number) => String(Number(v.toFixed(2)));

/**
 * One evidence series, drawn from the frozen snapshot.
 *
 * The picture is inline SVG rather than a charting engine: evidence is a fixed
 * handful of points read once, and pulling a chart library into the room would
 * cost the whole feature its bundle budget for a shape this small. The label
 * carries the same reading in words, so the series is legible without the
 * picture.
 */
export function Sparkline({ dim, points }: { readonly dim: string; readonly points: readonly Reading[] }) {
  const shape = sparklineShape(points, WIDTH, HEIGHT);

  if (!shape) {
    const only = points.at(0);
    return (
      <p className="text-xs text-gray-500">
        {dim} — {only ? `one reading: ${trim(only.value)}` : 'no readings recorded'}
      </p>
    );
  }

  return (
    <svg
      role="img"
      aria-label={`${dim} over the window — ${trim(shape.min)} to ${trim(shape.max)}, ending at ${trim(shape.last)}`}
      viewBox={`0 0 ${String(WIDTH)} ${String(HEIGHT)}`}
      className="w-full h-10"
      preserveAspectRatio="none"
    >
      <polyline
        points={shape.polyline}
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        vectorEffect="non-scaling-stroke"
        className="text-sky-400"
      />
    </svg>
  );
}
