import { healthBand, HEALTH_META, formatAnomalyPct } from './health';

interface HealthBadgeProps {
  readonly anomalyRate: number | null | undefined;
  /** Shows the raw percentage in place of the band label. */
  readonly showPct?: boolean;
  readonly className?: string;
}

/** HealthBadge is a coloured dot plus the health band or percentage, one cheap element per card. */
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
