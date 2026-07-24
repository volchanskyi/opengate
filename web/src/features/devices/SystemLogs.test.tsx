import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { SystemLogs } from './SystemLogs';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
  },
}));

const hostLogs = {
  entries: [
    { timestamp: '2026-04-01T12:00:00Z', level: 'INFO', target: 'sshd.service', message: 'accepted login' },
    { timestamp: '2026-04-01T12:01:00Z', level: 'ERROR', target: 'nginx.service', message: 'connection reset' },
  ],
  total: 2,
  has_more: false,
  available_units: ['nginx.service', 'sshd.service'],
};

function setSystemLogs(logs: typeof hostLogs | { entries: never[]; total: number; has_more: boolean; available_units?: string[] } | null, loading = false) {
  useDeviceStore.setState({
    logs: { agent: null, system: logs },
    logsLoading: { agent: false, system: loading },
  });
}

describe('SystemLogs (Host pane over LogExplorer)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      logs: { agent: null, system: null },
      logsLoading: { agent: false, system: false },
      fetchLogs: vi.fn(),
    });
  });

  it('renders the System Logs header', () => {
    render(<SystemLogs deviceId="d1" />);
    expect(screen.getByText('System Logs')).toBeInTheDocument();
  });

  it('populates the unit dropdown from available_units', () => {
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);
    const unit = screen.getByLabelText('Unit') as HTMLSelectElement;
    const labels = Array.from(unit.options).map((o) => o.textContent);
    expect(labels).toEqual(['All units', 'nginx.service', 'sshd.service']);
  });

  it('renders the target column and each entry unit', () => {
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);
    expect(screen.getByRole('button', { name: 'nginx.service' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'sshd.service' })).toBeInTheDocument();
  });

  it('fetches the host source with the selected unit', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);

    await user.selectOptions(screen.getByLabelText('Unit'), 'nginx.service');

    expect(fetchLogs).toHaveBeenLastCalledWith('system', 'd1', expect.objectContaining({
      unit: 'nginx.service',
      offset: 0,
    }));
  });

  it('clicking a target cell filters to that unit', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);

    await user.click(screen.getByRole('button', { name: 'nginx.service' }));

    expect(fetchLogs).toHaveBeenLastCalledWith('system', 'd1', expect.objectContaining({ unit: 'nginx.service' }));
    // The dropdown now reflects the click-selected unit.
    expect((screen.getByLabelText('Unit') as HTMLSelectElement).value).toBe('nginx.service');
  });

  it('a range button carries the host source even with no unit chosen', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<SystemLogs deviceId="d1" />);

    await user.click(screen.getByRole('button', { name: '1h' }));

    const [source, id, args] = fetchLogs.mock.calls[0]!;
    expect(source).toBe('system');
    expect(id).toBe('d1');
    expect(args.unit).toBeUndefined();
  });

  it('shows only "All units" when the host reports no units', () => {
    setSystemLogs({ entries: [], total: 0, has_more: false, available_units: [] });
    render(<SystemLogs deviceId="d1" />);
    const unit = screen.getByLabelText('Unit') as HTMLSelectElement;
    expect(Array.from(unit.options).map((o) => o.textContent)).toEqual(['All units']);
  });

  it('renders a dash for an entry with no unit target', () => {
    setSystemLogs({
      entries: [{ timestamp: 't', level: 'INFO', target: '', message: 'no unit' }],
      total: 1,
      has_more: false,
      available_units: [],
    } as never);
    render(<SystemLogs deviceId="d1" />);
    expect(screen.getByText('—')).toBeInTheDocument();
  });
});
