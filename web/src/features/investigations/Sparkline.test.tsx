import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { Sparkline } from './Sparkline';

describe('Sparkline', () => {
  it('draws the readings and says in words what the picture shows', () => {
    render(<Sparkline dim="cpu.busy_pct" points={[{ ts: 1, value: 40 }, { ts: 2, value: 96 }]} />);

    const chart = screen.getByRole('img', { name: /cpu\.busy_pct over the window/i });
    expect(chart).toHaveAccessibleName(/40 to 96/);
    expect(chart).toHaveAccessibleName(/ending at 96/);
    expect(chart.querySelector('polyline')).toHaveAttribute('points');
  });

  it('rounds a long reading in the label rather than printing every digit', () => {
    render(<Sparkline dim="disk.await_ms" points={[{ ts: 1, value: 4.123456 }, { ts: 2, value: 190.987 }]} />);
    expect(screen.getByRole('img', { name: /disk\.await_ms/ })).toHaveAccessibleName(/4\.12 to 190\.99/);
  });

  it('renders one reading as a number, because a single point is not a line', () => {
    render(<Sparkline dim="cpu.busy_pct" points={[{ ts: 1, value: 96 }]} />);
    expect(screen.getByText(/one reading: 96/i)).toBeInTheDocument();
    expect(screen.queryByRole('img')).toBeNull();
  });

  it('says a series recorded nothing rather than drawing an empty box', () => {
    render(<Sparkline dim="cpu.busy_pct" points={[]} />);
    expect(screen.getByText(/no readings/i)).toBeInTheDocument();
  });
});
