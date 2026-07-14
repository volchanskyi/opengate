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
    render(<FleetHealth devices={[
      device('a', 0.8), device('b', 0.9), device('c', 0.15), device('d', 0.01), device('e'),
    ]} />);
    expect(screen.getByText('Anomalous').closest('div')).toHaveTextContent('2');
    expect(screen.getByText('Watch').closest('div')).toHaveTextContent('1');
    expect(screen.getByText('Healthy').closest('div')).toHaveTextContent('1');
    expect(screen.getByText('No data').closest('div')).toHaveTextContent('1');

    const figure = screen.getByLabelText('Fleet health distribution');
    const bars = [...figure.children] as HTMLElement[];
    expect(bars).toHaveLength(3);
    expect(bars.map((bar) => bar.style.width)).toEqual(['50%', '25%', '25%']);
  });

  it('renders a distribution bar when at least one device is monitored', () => {
    render(<FleetHealth devices={[device('a', 0.8)]} />);
    const figure = screen.getByLabelText('Fleet health distribution');
    expect(figure).toBeInTheDocument();
    expect(figure.children[0]).toHaveStyle({ width: '100%' });
    expect(figure.children).toHaveLength(1);
  });

  it('shows an empty message when no device has telemetry', () => {
    render(<FleetHealth devices={[device('a'), device('b')]} />);
    expect(screen.getByText(/no edge telemetry yet/i)).toBeInTheDocument();
    expect(screen.queryByLabelText('Fleet health distribution')).toBeNull();
  });
});
