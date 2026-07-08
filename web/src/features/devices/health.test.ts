import { describe, it, expect } from 'vitest';
import { healthBand, formatAnomalyPct, HEALTH_META, WATCH_THRESHOLD, ANOMALOUS_THRESHOLD, type HealthMeta } from './health';

describe('healthBand', () => {
  it('classifies a low anomaly rate as healthy', () => {
    expect(healthBand(0)).toBe('healthy');
    expect(healthBand(WATCH_THRESHOLD - 0.001)).toBe('healthy');
  });

  it('classifies a mid anomaly rate as watch', () => {
    expect(healthBand(WATCH_THRESHOLD)).toBe('watch');
    expect(healthBand(ANOMALOUS_THRESHOLD - 0.001)).toBe('watch');
  });

  it('classifies a high anomaly rate as anomalous', () => {
    expect(healthBand(ANOMALOUS_THRESHOLD)).toBe('anomalous');
    expect(healthBand(1)).toBe('anomalous');
  });

  it('treats a missing / non-finite rate as unknown', () => {
    expect(healthBand(undefined)).toBe('unknown');
    expect(healthBand(null)).toBe('unknown');
    expect(healthBand(Number.NaN)).toBe('unknown');
  });
});

describe('formatAnomalyPct', () => {
  it('renders a percentage', () => {
    expect(formatAnomalyPct(0.5)).toBe('50%');
    expect(formatAnomalyPct(0.05)).toBe('5.0%');
  });

  it('renders an em dash when there is no sample', () => {
    expect(formatAnomalyPct(null)).toBe('—');
    expect(formatAnomalyPct(undefined)).toBe('—');
  });
});

describe('HEALTH_META', () => {
  it('has a well-formed entry for every band', () => {
    expect(Object.keys(HEALTH_META).sort()).toEqual(['anomalous', 'healthy', 'unknown', 'watch']);
    const metas: HealthMeta[] = Object.values(HEALTH_META);
    for (const meta of metas) {
      expect(meta.label).toBeTruthy();
      expect(meta.dotClass).toMatch(/^bg-/);
    }
  });
});
