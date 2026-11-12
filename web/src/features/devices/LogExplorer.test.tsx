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

  it('the next page starts after the last line the machine sent, so a shortened page skips nothing', async () => {
    const user = userEvent.setup();
    const entries = Array.from({ length: 37 }, (_, i) => ({
      timestamp: `2026-11-11T04:30:${String(i).padStart(2, '0')}Z`,
      level: 'ERROR',
      target: 'tomcat',
      message: `at org.apache.catalina.core.StandardWrapperValve.invoke ${i}`,
    }));
    useDeviceStore.setState({
      logs: { agent: { entries, total: 300, has_more: true }, system: null },
    });
    render(<LogExplorer deviceId="d1" source="agent" title="Agent Logs" />);
    await user.click(screen.getByRole('button', { name: '1h' }));
    const fetchLogs = vi.mocked(useDeviceStore.getState().fetchLogs);
    fetchLogs.mockClear();

    await user.click(screen.getByRole('button', { name: 'Next page' }));

    expect(fetchLogs).toHaveBeenCalledWith('agent', 'd1', expect.objectContaining({ offset: 37 }));
  });
});
