import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { MemoryRouter } from 'react-router';
import type { components } from '../../types/api';
import { FleetHealth } from './FleetHealth';

type FleetHealthCounts = components['schemas']['FleetHealthCounts'];

function renderFleetHealth(counts: FleetHealthCounts) {
  return render(<MemoryRouter><FleetHealth counts={counts} /></MemoryRouter>);
}

describe('FleetHealth', () => {
  it('renders the server-counted bands', () => {
    renderFleetHealth({ anomalous: 2, watch: 1, healthy: 1, unknown: 1 });
    expect(screen.getByText('Anomalous').closest('a')).toHaveTextContent('2');
    expect(screen.getByText('Watch').closest('a')).toHaveTextContent('1');
    expect(screen.getByText('Healthy').closest('a')).toHaveTextContent('1');
    expect(screen.getByText('No data').closest('a')).toHaveTextContent('1');

    const figure = screen.getByLabelText('Fleet health distribution');
    const bars = [...figure.children] as HTMLElement[];
    expect(bars).toHaveLength(3);
    expect(bars.map((bar) => bar.style.width)).toEqual(['50%', '25%', '25%']);
  });

  it('each band card deep-links to the matching device-list health filter', () => {
    renderFleetHealth({ anomalous: 1, watch: 1, healthy: 1, unknown: 1 });
    expect(screen.getByText('Anomalous').closest('a')).toHaveAttribute('href', '/devices?health=anomalous');
    expect(screen.getByText('Watch').closest('a')).toHaveAttribute('href', '/devices?health=watch');
    expect(screen.getByText('Healthy').closest('a')).toHaveAttribute('href', '/devices?health=healthy');
    expect(screen.getByText('No data').closest('a')).toHaveAttribute('href', '/devices?health=unknown');
  });

  it('renders a distribution bar when at least one device is monitored', () => {
    renderFleetHealth({ anomalous: 1, watch: 0, healthy: 0, unknown: 0 });
    const figure = screen.getByLabelText('Fleet health distribution');
    expect(figure).toBeInTheDocument();
    expect(figure.children[0]).toHaveStyle({ width: '100%' });
    expect(figure.children).toHaveLength(1);
  });

  it('shows an empty message when no device has telemetry', () => {
    renderFleetHealth({ anomalous: 0, watch: 0, healthy: 0, unknown: 2 });
    expect(screen.getByText(/no edge telemetry yet/i)).toBeInTheDocument();
    expect(screen.queryByLabelText('Fleet health distribution')).toBeNull();
  });

  it('renders zeroes for an empty fleet without a distribution bar', () => {
    renderFleetHealth({ anomalous: 0, watch: 0, healthy: 0, unknown: 0 });
    expect(screen.getByText(/no edge telemetry yet/i)).toBeInTheDocument();
  });
});
