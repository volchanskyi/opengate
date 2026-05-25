import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { DeviceLogs } from './DeviceLogs';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
  },
}));

const sampleLogs = {
  entries: [
    { timestamp: '2026-04-01T12:00:00Z', level: 'INFO', target: 'mesh_agent::main', message: 'agent started' },
    { timestamp: '2026-04-01T12:01:00Z', level: 'WARN', target: 'mesh_agent::connection', message: 'slow heartbeat' },
    { timestamp: '2026-04-01T12:02:00Z', level: 'ERROR', target: 'mesh_agent::connection', message: 'connection lost' },
  ],
  total: 3,
  has_more: false,
};

describe('DeviceLogs', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      logs: null,
      logsLoading: false,
      fetchLogs: vi.fn(),
    });
  });

  it('renders fetch button', () => {
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Fetch Logs')).toBeInTheDocument();
  });

  it('displays log entries with level color coding', () => {
    useDeviceStore.setState({ logs: sampleLogs });
    render(<DeviceLogs deviceId="d1" />);

    expect(screen.getByText('agent started')).toBeInTheDocument();
    expect(screen.getByText('slow heartbeat')).toBeInTheDocument();
    expect(screen.getByText('connection lost')).toBeInTheDocument();

    // Check level text is present
    const errorCells = screen.getAllByText(/ERROR/);
    expect(errorCells.length).toBeGreaterThan(0);
    const warnCells = screen.getAllByText(/WARN/);
    expect(warnCells.length).toBeGreaterThan(0);
  });

  it('shows filter bar with level dropdown and search', () => {
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('All Levels')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Search keyword...')).toBeInTheDocument();
  });

  it('shows entry count', () => {
    useDeviceStore.setState({ logs: sampleLogs });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Showing 1-3 of 3')).toBeInTheDocument();
  });

  it('shows loading state while fetching', () => {
    useDeviceStore.setState({ logsLoading: true });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Fetching...')).toBeInTheDocument();
  });

  it('shows empty state when no logs', () => {
    useDeviceStore.setState({ logs: { entries: [], total: 0, has_more: false } });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('No logs available')).toBeInTheDocument();
  });

  it('fetch button disabled while loading', () => {
    useDeviceStore.setState({ logsLoading: true });
    render(<DeviceLogs deviceId="d1" />);
    const button = screen.getByText('Fetching...');
    expect(button).toBeDisabled();
  });

  it('calls fetchLogs on button click', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });

    render(<DeviceLogs deviceId="d1" />);
    await user.click(screen.getByText('Fetch Logs'));

    expect(fetchLogs).toHaveBeenCalledWith('d1', expect.objectContaining({ offset: 0, limit: 300, refresh: true }));
  });

  it('shows Load More button when has_more is true', () => {
    useDeviceStore.setState({
      logs: { ...sampleLogs, has_more: true, total: 150 },
    });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Load More')).toBeInTheDocument();
  });

  it('Load More increments offset and fetches next page', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({
      fetchLogs,
      logs: { ...sampleLogs, has_more: true, total: 600 },
    });
    render(<DeviceLogs deviceId="d1" />);

    await user.click(screen.getByText('Load More'));

    expect(fetchLogs).toHaveBeenCalledWith('d1', expect.objectContaining({
      offset: 300,
      limit: 300,
    }));
  });

  it('passes level and search filters to fetchLogs', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<DeviceLogs deviceId="d1" />);

    // Set level filter
    await user.selectOptions(screen.getByDisplayValue('All Levels'), 'ERROR');
    // Set search filter
    await user.type(screen.getByPlaceholderText('Search keyword...'), 'timeout');

    await user.click(screen.getByText('Fetch Logs'));

    expect(fetchLogs).toHaveBeenCalledWith('d1', expect.objectContaining({
      level: 'ERROR',
      search: 'timeout',
      offset: 0,
      refresh: true,
    }));
  });

  it('empty level/search are passed as undefined (not empty string) to fetchLogs', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<DeviceLogs deviceId="d1" />);

    await user.click(screen.getByText('Fetch Logs'));

    expect(fetchLogs).toHaveBeenCalledTimes(1);
    const [, args] = fetchLogs.mock.calls[0]!;
    expect(args.level).toBeUndefined();
    expect(args.search).toBeUndefined();
  });

  it('level entry uses red color class for ERROR rows', () => {
    useDeviceStore.setState({ logs: sampleLogs });
    render(<DeviceLogs deviceId="d1" />);
    // Find the cell containing ERROR padded with whitespace
    const errorCell = screen.getAllByText(/ERROR/).find((el) => el.tagName === 'TD');
    expect(errorCell?.className).toContain('text-red-400');
  });

  it('level entry uses gray-400 fallback class for unknown level', () => {
    useDeviceStore.setState({
      logs: { ...sampleLogs, entries: [
        { timestamp: 't1', level: 'UNKNOWN', target: 'x', message: 'weird' },
      ] },
    });
    render(<DeviceLogs deviceId="d1" />);
    const td = screen.getByText(/UNKNOWN/);
    expect(td.className).toContain('text-gray-400');
  });

  it('Showing total clamps to logs.total (Math.min branch)', () => {
    useDeviceStore.setState({
      logs: { ...sampleLogs, total: 5 },
    });
    render(<DeviceLogs deviceId="d1" />);
    // offset 0 + 3 entries = 3, total is 5, Math.min(3, 5) = 3 → "Showing 1-3 of 5"
    expect(screen.getByText('Showing 1-3 of 5')).toBeInTheDocument();
  });

  it('Load More hidden when has_more is false', () => {
    useDeviceStore.setState({ logs: { ...sampleLogs, has_more: false } });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.queryByText('Load More')).toBeNull();
  });

  it('Load More button is disabled while another fetch is in flight', () => {
    useDeviceStore.setState({
      logs: { ...sampleLogs, has_more: true, total: 100 },
      logsLoading: true,
    });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Load More')).toBeDisabled();
  });

  it('level dropdown contains all five named levels plus "All Levels"', () => {
    render(<DeviceLogs deviceId="d1" />);
    const select = screen.getByDisplayValue('All Levels') as HTMLSelectElement;
    const labels = Array.from(select.options).map((o) => o.textContent);
    expect(labels).toEqual(['All Levels', 'TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR']);
  });
});
