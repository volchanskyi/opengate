const UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const;

/**
 * Render a byte count in the largest unit that keeps it a small number.
 *
 * One decimal below 100 and none above it: "1.5 KB" carries information a
 * reader uses, "153.6 KB" spends a character on precision nobody acts on.
 */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const idx = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), UNITS.length - 1);
  const val = bytes / Math.pow(1024, idx);
  return `${val.toFixed(val >= 100 ? 0 : 1)} ${UNITS.at(idx) ?? 'B'}`;
}
