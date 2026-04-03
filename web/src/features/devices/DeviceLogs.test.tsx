import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useDeviceStore } from '../../state/device-store';
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

    expect(fetchLogs).toHaveBeenCalledWith('d1', expect.objectContaining({ offset: 0, limit: 300 }));
  });

  it('shows Load More button when has_more is true', () => {
    useDeviceStore.setState({
      logs: { ...sampleLogs, has_more: true, total: 150 },
    });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Load More')).toBeInTheDocument();
  });
});
