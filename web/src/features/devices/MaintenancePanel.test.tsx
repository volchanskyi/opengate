import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import type { components } from '../../types/api';
import { MaintenancePanel } from './MaintenancePanel';

type Device = components['schemas']['Device'];

const DAY = 86_400_000;
const daysAgo = (n: number) => new Date(Date.now() - n * DAY).toISOString();

function device(over: Partial<Device> = {}): Device {
  return {
    id: 'd1', group_id: 'g1', hostname: 'host1', os: 'linux', agent_version: '1.0.0',
    capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '', ...over,
  };
}

describe('MaintenancePanel — active device', () => {
  it('offers to enter maintenance with an optional reason', () => {
    render(<MaintenancePanel device={device()} onToggle={vi.fn()} />);
    expect(screen.getByRole('button', { name: /enter maintenance/i })).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/reason/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /exit maintenance/i })).toBeNull();
  });

  it('passes the typed reason when entering', async () => {
    const onToggle = vi.fn().mockResolvedValue(true);
    const user = userEvent.setup();
    render(<MaintenancePanel device={device()} onToggle={onToggle} />);

    await user.type(screen.getByPlaceholderText(/reason/i), 'kernel upgrade');
    await user.click(screen.getByRole('button', { name: /enter maintenance/i }));

    expect(onToggle).toHaveBeenCalledWith(true, 'kernel upgrade');
  });

  it('omits a blank reason (passes undefined)', async () => {
    const onToggle = vi.fn().mockResolvedValue(true);
    const user = userEvent.setup();
    render(<MaintenancePanel device={device()} onToggle={onToggle} />);

    await user.click(screen.getByRole('button', { name: /enter maintenance/i }));

    expect(onToggle).toHaveBeenCalledWith(true, undefined);
  });

  it('clears the reason field after a successful enter', async () => {
    const onToggle = vi.fn().mockResolvedValue(true);
    const user = userEvent.setup();
    render(<MaintenancePanel device={device()} onToggle={onToggle} />);

    const input = screen.getByPlaceholderText(/reason/i);
    await user.type(input, 'patching');
    await user.click(screen.getByRole('button', { name: /enter maintenance/i }));

    expect(input).toHaveValue('');
  });
});

describe('MaintenancePanel — device in maintenance', () => {
  it('shows the since timestamp and offers to exit', () => {
    const since = daysAgo(0);
    render(<MaintenancePanel device={device({ maintenance_on: true, maintenance_since: since })} onToggle={vi.fn()} />);
    const banner = screen.getByText(/in maintenance since/i);
    expect(banner).toHaveTextContent(new Date(since).toLocaleString());
    expect(screen.getByRole('button', { name: /exit maintenance/i })).toBeInTheDocument();
  });

  it('surfaces the operator reason when present', () => {
    render(<MaintenancePanel device={device({ maintenance_on: true, maintenance_since: daysAgo(0), maintenance_reason: 'disk swap' })} onToggle={vi.fn()} />);
    expect(screen.getByText(/disk swap/)).toBeInTheDocument();
  });

  it('calls onToggle(false) when exiting', async () => {
    const onToggle = vi.fn().mockResolvedValue(true);
    const user = userEvent.setup();
    render(<MaintenancePanel device={device({ maintenance_on: true, maintenance_since: daysAgo(0) })} onToggle={onToggle} />);

    await user.click(screen.getByRole('button', { name: /exit maintenance/i }));

    expect(onToggle).toHaveBeenCalledWith(false, undefined);
  });

  it('does not raise an escalated alert for a fresh window', () => {
    render(<MaintenancePanel device={device({ maintenance_on: true, maintenance_since: daysAgo(0) })} onToggle={vi.fn()} />);
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('escalates an alert counting the days once past the warn threshold', () => {
    render(<MaintenancePanel device={device({ maintenance_on: true, maintenance_since: daysAgo(9) })} onToggle={vi.fn()} />);
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent(/9 days/);
  });
});
