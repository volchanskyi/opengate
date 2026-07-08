import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { HealthBadge } from './HealthBadge';

describe('HealthBadge', () => {
  it('shows Anomalous for a high anomaly rate', () => {
    render(<HealthBadge anomalyRate={0.8} />);
    expect(screen.getByText('Anomalous')).toBeInTheDocument();
  });

  it('shows Healthy for a low anomaly rate', () => {
    render(<HealthBadge anomalyRate={0.01} />);
    expect(screen.getByText('Healthy')).toBeInTheDocument();
  });

  it('shows "No data" when there is no recent sample', () => {
    render(<HealthBadge anomalyRate={undefined} />);
    expect(screen.getByText('No data')).toBeInTheDocument();
  });

  it('renders the percentage instead of the label when showPct is set', () => {
    render(<HealthBadge anomalyRate={0.5} showPct />);
    expect(screen.getByText('50%')).toBeInTheDocument();
    expect(screen.queryByText('Anomalous')).toBeNull();
  });

  it('surfaces the anomaly percentage in the hover title', () => {
    render(<HealthBadge anomalyRate={0.5} />);
    expect(screen.getByText('Anomalous').getAttribute('title')).toContain('50%');
  });
});
