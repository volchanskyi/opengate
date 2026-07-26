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

describe('MaintenanceBadge tooltip duration', () => {
  it('omits the duration for a sub-day window', () => {
    const since = daysAgo(0);
    render(<MaintenanceBadge since={since} />);
    const title = screen.getByText('Maintenance').getAttribute('title');
    expect(title).toBe(`In maintenance since ${new Date(since).toLocaleString()}`);
  });

  it('appends a singular duration on the first whole day', () => {
    render(<MaintenanceBadge since={daysAgo(1)} />);
    expect(screen.getByText('Maintenance').getAttribute('title')).toContain('(for 1 day)');
  });

  it('appends a pluralised duration beyond the first day', () => {
    render(<MaintenanceBadge since={daysAgo(3)} />);
    expect(screen.getByText('Maintenance').getAttribute('title')).toContain('(for 3 days)');
  });

  it('falls back to a bare tooltip when no timestamp is known', () => {
    render(<MaintenanceBadge />);
    expect(screen.getByText('Maintenance').getAttribute('title')).toBe('In maintenance');
  });
});

describe('MaintenanceBadge styling', () => {
  it('renders the escalation dot with the severity colour', () => {
    const { container } = render(<MaintenanceBadge since={daysAgo(9)} />);
    const dot = container.querySelector('[aria-hidden="true"]');
    expect(dot).not.toBeNull();
    expect(dot).toHaveClass('w-1.5', 'h-1.5', 'rounded-full', 'bg-red-500');
  });

  it('adds no stray classes when no className is supplied', () => {
    render(<MaintenanceBadge since={daysAgo(0)} />);
    const cls = screen.getByText('Maintenance').getAttribute('class') ?? '';
    expect(cls.trim().split(/\s+/)).toEqual([
      'inline-flex', 'items-center', 'gap-1.5', 'rounded', 'px-1.5', 'py-0.5',
      'text-xs', 'font-medium', 'bg-sky-900/40', 'text-sky-300', 'border', 'border-sky-700',
    ]);
  });

  it('appends a caller-supplied className last', () => {
    render(<MaintenanceBadge since={daysAgo(0)} className="ml-2" />);
    expect(screen.getByText('Maintenance').getAttribute('class')).toContain('border-sky-700 ml-2');
  });
});
