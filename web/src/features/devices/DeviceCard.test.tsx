import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
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

function renderCard() {
  const router = createMemoryRouter(
    [
      { path: '/', element: <DeviceCard device={mockDevice} /> },
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
});

void vi;
