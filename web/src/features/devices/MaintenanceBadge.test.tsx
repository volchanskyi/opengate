import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { MaintenanceBadge } from './MaintenanceBadge';

const DAY = 86_400_000;
const daysAgo = (n: number) => new Date(Date.now() - n * DAY).toISOString();

describe('MaintenanceBadge', () => {
  it('renders the Maintenance label', () => {
    render(<MaintenanceBadge since={daysAgo(0)} />);
    expect(screen.getByText('Maintenance')).toBeInTheDocument();
  });

  it('exposes the since timestamp in its tooltip', () => {
    const since = daysAgo(1);
    render(<MaintenanceBadge since={since} />);
    const badge = screen.getByText('Maintenance');
    expect(badge.getAttribute('title')).toContain(new Date(since).toLocaleString());
  });

  it('uses the normal (sky) styling for a fresh window', () => {
    render(<MaintenanceBadge since={daysAgo(0)} />);
    expect(screen.getByText('Maintenance')).toHaveClass('text-sky-300');
  });

  it('escalates to amber after the warn threshold', () => {
    render(<MaintenanceBadge since={daysAgo(4)} />);
    expect(screen.getByText('Maintenance')).toHaveClass('text-amber-300');
  });

  it('escalates to red after the stale threshold', () => {
    render(<MaintenanceBadge since={daysAgo(9)} />);
    expect(screen.getByText('Maintenance')).toHaveClass('text-red-300');
  });

  it('still renders with no since timestamp (state present, clock absent)', () => {
    render(<MaintenanceBadge />);
    expect(screen.getByText('Maintenance')).toBeInTheDocument();
  });
});
