import { healthBand, HEALTH_META, formatAnomalyPct } from './health';

interface HealthBadgeProps {
  readonly anomalyRate: number | null | undefined;
  /** Show the raw percentage instead of the band label. */
  readonly showPct?: boolean;
  readonly className?: string;
}

/**
 * Scalar edge-health badge — a coloured dot plus the health band (or percentage).
 * Deliberately not a sparkline: the virtualized grid renders one of these per
 * card, so it stays a single cheap element per device.
 */
export function HealthBadge({ anomalyRate, showPct = false, className = '' }: HealthBadgeProps) {
  const meta = HEALTH_META[healthBand(anomalyRate)];
  return (
    <span
      className={`inline-flex items-center gap-1 text-xs ${meta.textClass} ${className}`}
      title={`Anomaly rate: ${formatAnomalyPct(anomalyRate)}`}
    >
      <span className={`w-2 h-2 rounded-full ${meta.dotClass}`} aria-hidden="true" />
      {showPct ? formatAnomalyPct(anomalyRate) : meta.label}
    </span>
  );
}
