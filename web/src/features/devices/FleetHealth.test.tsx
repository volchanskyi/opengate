import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import type { components } from '../../types/api';
import { FleetHealth } from './FleetHealth';

type Device = components['schemas']['Device'];

function device(id: string, anomalyRate?: number): Device {
  return {
    id,
    group_id: 'g1',
    hostname: id,
    os: 'linux',
    agent_version: '1.0.0',
    status: 'online',
    capabilities: [],
    last_seen: '',
    created_at: '',
    updated_at: '',
    ...(anomalyRate === undefined ? {} : { anomaly_rate: anomalyRate }),
  };
}

describe('FleetHealth', () => {
  it('counts devices into health bands', () => {
    render(<FleetHealth devices={[device('a', 0.8), device('b', 0.15), device('c', 0.01), device('d')]} />);
    // one anomalous, one watch, one healthy, one no-data
    const anomalous = screen.getByText('Anomalous').closest('div');
    expect(anomalous).toHaveTextContent('1');
    const healthy = screen.getByText('Healthy').closest('div');
    expect(healthy).toHaveTextContent('1');
  });

  it('renders a distribution bar when at least one device is monitored', () => {
    render(<FleetHealth devices={[device('a', 0.8)]} />);
    expect(screen.getByLabelText('Fleet health distribution')).toBeInTheDocument();
  });

  it('shows an empty message when no device has telemetry', () => {
    render(<FleetHealth devices={[device('a'), device('b')]} />);
    expect(screen.getByText(/no edge telemetry yet/i)).toBeInTheDocument();
    expect(screen.queryByLabelText('Fleet health distribution')).toBeNull();
  });
});
