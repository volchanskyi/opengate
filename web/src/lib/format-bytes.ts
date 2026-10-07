const UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const;

/** Renders a byte count in the largest unit that keeps it small, with one decimal below 100. */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const idx = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), UNITS.length - 1);
  const val = bytes / Math.pow(1024, idx);
  return `${val.toFixed(val >= 100 ? 0 : 1)} ${UNITS.at(idx) ?? 'B'}`;
}
