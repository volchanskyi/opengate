import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { DeviceCard } from './DeviceCard';

const mockDevice = {
  id: 'd1',
  group_id: 'g1',
  hostname: 'test-host',
  os: 'linux',
  agent_version: '1.0.0',
  status: 'online' as const,
  last_seen: new Date().toISOString(),
  capabilities: [],
  created_at: '',
  updated_at: '',
};

function renderCard(overrides: Partial<typeof mockDevice> = {}) {
  const router = createMemoryRouter(
    [
      { path: '/', element: <DeviceCard device={{ ...mockDevice, ...overrides }} /> },
      { path: '/devices/:id', element: <p>Device Detail</p> },
    ],
    { initialEntries: ['/'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('DeviceCard', () => {
  it('renders hostname and OS', () => {
    renderCard();
    expect(screen.getByText('test-host')).toBeInTheDocument();
    expect(screen.getByText('OS: linux')).toBeInTheDocument();
  });

  it('shows status badge', () => {
    renderCard();
    expect(screen.getByText('Online')).toBeInTheDocument();
  });

  it('navigates to device detail on click', async () => {
    const user = userEvent.setup();
    renderCard();
    await user.click(screen.getByText('test-host'));
    expect(screen.getByText('Device Detail')).toBeInTheDocument();
  });

  describe('timeAgo formatting', () => {
    // Pin Date.now() so the timeAgo branches are deterministic.
    const NOW = new Date('2026-05-08T12:00:00Z').getTime();
    beforeEach(() => {
      vi.useFakeTimers();
      vi.setSystemTime(NOW);
    });
    afterEach(() => vi.useRealTimers());

    it('"just now" for last_seen <60s ago', () => {
      const lastSeen = new Date(NOW - 30 * 1000).toISOString();
      renderCard({ last_seen: lastSeen });
      // Pins string literal 'just now' — kills StringLiteral mutant.
      expect(screen.getByText('Last seen: just now')).toBeInTheDocument();
    });

    it('"Xm ago" for 1–59 minutes ago (kills `seconds < 60` boundary mutants)', () => {
      const lastSeen = new Date(NOW - 5 * 60 * 1000).toISOString(); // 5 min
      renderCard({ last_seen: lastSeen });
      expect(screen.getByText('Last seen: 5m ago')).toBeInTheDocument();
    });

    it('"1m ago" exactly at 60s — kills `<` → `<=` boundary mutant on seconds', () => {
      const lastSeen = new Date(NOW - 60 * 1000).toISOString();
      renderCard({ last_seen: lastSeen });
      // At seconds=60 the function falls into the minutes branch: 1m ago,
      // not "just now".
      expect(screen.getByText('Last seen: 1m ago')).toBeInTheDocument();
    });

    it('"Xh ago" for 1–23 hours ago', () => {
      const lastSeen = new Date(NOW - 3 * 60 * 60 * 1000).toISOString(); // 3 hours
      renderCard({ last_seen: lastSeen });
      expect(screen.getByText('Last seen: 3h ago')).toBeInTheDocument();
    });

    it('"1h ago" exactly at 60 min — kills `<` → `<=` boundary mutant on minutes', () => {
      const lastSeen = new Date(NOW - 60 * 60 * 1000).toISOString();
      renderCard({ last_seen: lastSeen });
      expect(screen.getByText('Last seen: 1h ago')).toBeInTheDocument();
    });

    it('"Xd ago" for >=24 hours ago', () => {
      const lastSeen = new Date(NOW - 5 * 24 * 60 * 60 * 1000).toISOString();
      renderCard({ last_seen: lastSeen });
      // Kills `<` → `>=` mutant on hours: with arithmetic mutant `*` instead of `/`,
      // days would be `5 * 24 * 24 = 2880` not 5.
      expect(screen.getByText('Last seen: 5d ago')).toBeInTheDocument();
    });

    it('"1d ago" exactly at 24h — kills `<` → `<=` boundary mutant on hours', () => {
      const lastSeen = new Date(NOW - 24 * 60 * 60 * 1000).toISOString();
      renderCard({ last_seen: lastSeen });
      expect(screen.getByText('Last seen: 1d ago')).toBeInTheDocument();
    });
  });
});
