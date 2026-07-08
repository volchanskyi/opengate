/**
 * Edge-node health derived from the latest anomaly rate a device reported. The
 * rate is a scalar in [0,1]; the grid badge and the device-detail anomaly panel
 * both classify it into one of these bands. Thresholds are intentionally coarse
 * — this is an investigation aid, not an alerting signal.
 */
export type HealthBand = 'healthy' | 'watch' | 'anomalous' | 'unknown';

export const WATCH_THRESHOLD = 0.1;
export const ANOMALOUS_THRESHOLD = 0.3;

export function healthBand(rate: number | null | undefined): HealthBand {
  if (rate == null || !Number.isFinite(rate)) return 'unknown';
  if (rate >= ANOMALOUS_THRESHOLD) return 'anomalous';
  if (rate >= WATCH_THRESHOLD) return 'watch';
  return 'healthy';
}

export interface HealthMeta {
  label: string;
  /** Tailwind background class for the status dot. */
  dotClass: string;
  /** Tailwind text-colour class for the label. */
  textClass: string;
}

export const HEALTH_META: Record<HealthBand, HealthMeta> = {
  healthy: { label: 'Healthy', dotClass: 'bg-green-500', textClass: 'text-green-400' },
  watch: { label: 'Watch', dotClass: 'bg-amber-500', textClass: 'text-amber-400' },
  anomalous: { label: 'Anomalous', dotClass: 'bg-red-500', textClass: 'text-red-400' },
  unknown: { label: 'No data', dotClass: 'bg-gray-600', textClass: 'text-gray-500' },
};

/** Human-readable anomaly percentage, or an em dash when there is no sample. */
export function formatAnomalyPct(rate: number | null | undefined): string {
  if (rate == null || !Number.isFinite(rate)) return '—';
  return `${(rate * 100).toFixed(rate < WATCH_THRESHOLD ? 1 : 0)}%`;
}
