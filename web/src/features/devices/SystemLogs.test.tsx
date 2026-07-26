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

/** Open the collapsed-by-default output so its entries are reachable. */
async function expandPane(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Expand System Logs' }));
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

  it('starts with the output collapsed — controls are live, entries hidden, no fetch', () => {
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);

    expect(screen.getByRole('button', { name: 'Expand System Logs' })).toBeInTheDocument();
    // Only the output is collapsed: every control answers straight away.
    expect(screen.getByLabelText('Unit')).toBeInTheDocument();
    expect(screen.getByLabelText('Severity')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Search keyword...')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '1h' })).toBeInTheDocument();
    expect(screen.queryByText('accepted login')).toBeNull();
    expect(fetchLogs).not.toHaveBeenCalled();
  });

  it('populates the unit dropdown from available_units', async () => {
    const user = userEvent.setup();
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);
    await expandPane(user);

    const unit = screen.getByLabelText('Unit') as HTMLSelectElement;
    const labels = Array.from(unit.options).map((o) => o.textContent);
    expect(labels).toEqual(['All units', 'nginx.service', 'sshd.service']);
  });

  it('renders the target column and each entry unit', async () => {
    const user = userEvent.setup();
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);
    await expandPane(user);

    expect(screen.getByRole('button', { name: 'nginx.service' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'sshd.service' })).toBeInTheDocument();
  });

  it('fetches the host source with the selected unit', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);
    await expandPane(user);

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
    await expandPane(user);

    await user.click(screen.getByRole('button', { name: 'nginx.service' }));

    expect(fetchLogs).toHaveBeenLastCalledWith('system', 'd1', expect.objectContaining({ unit: 'nginx.service' }));
    // The dropdown now reflects the click-selected unit.
    expect((screen.getByLabelText('Unit') as HTMLSelectElement).value).toBe('nginx.service');
  });

  it('a range button carries the host source even with no unit chosen', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);
    await expandPane(user);

    await user.click(screen.getByRole('button', { name: '1h' }));

    const [source, id, args] = fetchLogs.mock.calls.at(-1)!;
    expect(source).toBe('system');
    expect(id).toBe('d1');
    expect(args.unit).toBeUndefined();
  });

  it('loads the default window once on the first expand when nothing is cached', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<SystemLogs deviceId="d1" />);

    // Mounting alone must not pull — opening a device page is not a log request.
    expect(fetchLogs).not.toHaveBeenCalled();

    await expandPane(user);
    expect(fetchLogs).toHaveBeenCalledTimes(1);
    const [source, id, args] = fetchLogs.mock.calls[0]!;
    expect(source).toBe('system');
    expect(id).toBe('d1');
    expect(args.offset).toBe(0);
    expect(typeof args.from).toBe('string');
    expect(typeof args.to).toBe('string');

    // Collapsing and re-expanding serves what is already loaded — every load
    // after the first is manual.
    await user.click(screen.getByRole('button', { name: 'Collapse System Logs' }));
    await expandPane(user);
    expect(fetchLogs).toHaveBeenCalledTimes(1);
  });

  it('serves the cache without a fetch when the pane already holds this device logs', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);
    await expandPane(user);

    expect(fetchLogs).not.toHaveBeenCalled();
    expect(screen.getByText('accepted login')).toBeInTheDocument();
  });

  it('has no manual refresh control — a window, filter or search drives every pull', async () => {
    const user = userEvent.setup();
    setSystemLogs(hostLogs);
    render(<SystemLogs deviceId="d1" />);
    await expandPane(user);

    expect(screen.queryByRole('button', { name: /refresh/i })).toBeNull();
  });

  it('a window click while collapsed opens the output and is the only pull', async () => {
    // The one automatic first-open pull must stand down once an explicit load
    // has run, or expanding-by-fetching would fire the same window twice.
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<SystemLogs deviceId="d1" />);

    await user.click(screen.getByRole('button', { name: '6h' }));

    expect(fetchLogs).toHaveBeenCalledTimes(1);
    const [, , args] = fetchLogs.mock.calls[0]!;
    expect(new Date(args.to).getTime() - new Date(args.from).getTime()).toBe(6 * 3600 * 1000);
    expect(screen.getByRole('button', { name: 'Collapse System Logs' })).toBeInTheDocument();
  });

  it('a correlation focusWindow expands the pane and drives the only fetch', () => {
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setSystemLogs(hostLogs);
    const win = { from: '2026-07-08T00:00:00Z', to: '2026-07-08T01:00:00Z' };
    render(<SystemLogs deviceId="d1" focusWindow={win} />);

    // Focus wins: exactly one fetch (the focus window), and the pane opens so
    // the drilled-to entries are visible without a second click.
    expect(fetchLogs).toHaveBeenCalledExactlyOnceWith('system', 'd1', expect.objectContaining({ from: win.from, to: win.to }));
    expect(screen.getByRole('button', { name: 'Collapse System Logs' })).toBeInTheDocument();
    expect(screen.getByText('accepted login')).toBeInTheDocument();
  });

  it('shows only "All units" when the host reports no units', async () => {
    const user = userEvent.setup();
    setSystemLogs({ entries: [], total: 0, has_more: false, available_units: [] });
    render(<SystemLogs deviceId="d1" />);
    await expandPane(user);

    const unit = screen.getByLabelText('Unit') as HTMLSelectElement;
    expect(Array.from(unit.options).map((o) => o.textContent)).toEqual(['All units']);
  });

  it('renders a dash for an entry with no unit target', async () => {
    const user = userEvent.setup();
    setSystemLogs({
      entries: [{ timestamp: 't', level: 'INFO', target: '', message: 'no unit' }],
      total: 1,
      has_more: false,
      available_units: [],
    } as never);
    render(<SystemLogs deviceId="d1" />);
    await expandPane(user);

    expect(screen.getByText('—')).toBeInTheDocument();
  });
});
