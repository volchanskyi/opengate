import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { LogExplorer } from './LogExplorer';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
  },
}));

describe('LogExplorer', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      logs: { agent: null, system: null },
      logsLoading: { agent: false, system: false },
      fetchLogs: vi.fn(),
    });
  });

  it('clearing the time window drops it and reads from the start without one', async () => {
    const user = userEvent.setup();
    render(<LogExplorer deviceId="d1" source="agent" title="Agent Logs" />);
    await user.click(screen.getByRole('button', { name: '1h' }));
    const fetchLogs = vi.mocked(useDeviceStore.getState().fetchLogs);
    fetchLogs.mockClear();

    await user.click(screen.getByRole('button', { name: /✕$/ }));

    expect(screen.queryByRole('button', { name: /✕$/ })).not.toBeInTheDocument();
    expect(fetchLogs).toHaveBeenCalledWith('agent', 'd1', expect.objectContaining({
      from: undefined,
      to: undefined,
      offset: 0,
    }));
  });
});
