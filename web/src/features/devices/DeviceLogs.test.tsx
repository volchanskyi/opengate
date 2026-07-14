import { fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, it, expect, beforeEach, vi } from 'vitest';
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

  afterEach(() => { vi.useRealTimers(); });

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

    await user.click(screen.getByText('Load More'));
    expect(fetchLogs).toHaveBeenLastCalledWith('d1', expect.objectContaining({ offset: 600 }));
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
    // The level also appears as a facet chip; target the table cell specifically.
    const td = screen.getAllByText(/UNKNOWN/).find((el) => el.tagName === 'TD');
    expect(td?.className).toContain('text-gray-400');
  });

  it('a time-range chip fetches a bounded window', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<DeviceLogs deviceId="d1" />);
    await user.click(screen.getByRole('button', { name: '1h' }));
    const [, args] = fetchLogs.mock.calls[0]!;
    expect(typeof args.from).toBe('string');
    expect(new Date(args.to).getTime() - new Date(args.from).getTime()).toBe(3600 * 1000);
  });

  it('clears an active window filter', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<DeviceLogs deviceId="d1" />);
    await user.click(screen.getByRole('button', { name: '6h' }));
    await user.click(screen.getByRole('button', { name: /✕/ }));
    const lastArgs = fetchLogs.mock.calls.at(-1)![1];
    expect(lastArgs.from).toBeUndefined();
    expect(lastArgs.to).toBeUndefined();
  });

  it('clicking a level facet chip quick-filters that level', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs, logs: sampleLogs });
    render(<DeviceLogs deviceId="d1" />);
    await user.click(screen.getByRole('button', { name: /ERROR 1/ }));
    expect(fetchLogs).toHaveBeenCalledWith('d1', expect.objectContaining({ level: 'ERROR', offset: 0 }));
  });

  it('orders facets by exact count and renders their inactive colors', () => {
    useDeviceStore.setState({
      logs: {
        entries: [
          ...sampleLogs.entries,
          { ...sampleLogs.entries[1]!, timestamp: 'w2' },
          { ...sampleLogs.entries[1]!, timestamp: 'w3' },
          { ...sampleLogs.entries[0]!, timestamp: 'i2' },
        ],
        total: 6,
        has_more: false,
      },
    });
    const { container } = render(<DeviceLogs deviceId="d1" />);

    const facetRow = screen.getByRole('button', { name: 'WARN 3' }).parentElement!;
    expect(within(facetRow).getAllByRole('button').map((button) => button.textContent)).toEqual([
      'WARN 3', 'INFO 2', 'ERROR 1',
    ]);
    expect(screen.getByRole('button', { name: 'WARN 3' })).toHaveClass(
      'bg-gray-700', 'hover:bg-gray-600', 'text-yellow-400',
    );
    expect(container.querySelectorAll('div.flex.items-center.gap-1.mb-2.flex-wrap')).toHaveLength(2);
  });

  it('toggles an active facet back to the all-level filter', async () => {
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs, logs: sampleLogs });
    render(<DeviceLogs deviceId="d1" />);
    const errorFacet = screen.getByRole('button', { name: 'ERROR 1' });

    await userEvent.click(errorFacet);
    expect(errorFacet).toHaveClass('bg-blue-600', 'text-white', 'text-red-400');
    expect(fetchLogs).toHaveBeenLastCalledWith('d1', expect.objectContaining({ level: 'ERROR' }));

    await userEvent.click(errorFacet);
    expect(errorFacet).toHaveClass('bg-gray-700', 'hover:bg-gray-600', 'text-red-400');
    expect(fetchLogs).toHaveBeenLastCalledWith('d1', expect.objectContaining({ level: undefined }));
  });

  it('omits the facet row when the page has no levels', () => {
    useDeviceStore.setState({ logs: { entries: [], total: 0, has_more: false } });
    const { container } = render(<DeviceLogs deviceId="d1" />);
    expect(container.querySelectorAll('div.flex.items-center.gap-1.mb-2.flex-wrap')).toHaveLength(1);
  });

  it.each([
    ['15m', '2026-07-14T18:45:00.000Z'],
    ['1h', '2026-07-14T18:00:00.000Z'],
    ['6h', '2026-07-14T13:00:00.000Z'],
    ['24h', '2026-07-13T19:00:00.000Z'],
  ])('maps the %s range to its exact UTC window', (label, from) => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-14T19:00:00.000Z'));
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<DeviceLogs deviceId="d1" />);

    fireEvent.click(screen.getByRole('button', { name: label }));

    expect(fetchLogs).toHaveBeenCalledExactlyOnceWith('d1', {
      level: undefined,
      search: undefined,
      from,
      to: '2026-07-14T19:00:00.000Z',
      offset: 0,
      limit: 300,
    });
  });

  it('correlation jump: a focusWindow pre-filters and fetches that window', () => {
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<DeviceLogs deviceId="d1" focusWindow={{ from: '2026-07-08T00:00:00Z', to: '2026-07-08T01:00:00Z' }} />);
    expect(fetchLogs).toHaveBeenCalledWith('d1', expect.objectContaining({
      from: '2026-07-08T00:00:00Z',
      to: '2026-07-08T01:00:00Z',
      offset: 0,
    }));
  });

  it('renders the exact focused-window label and refreshes the focus callback', async () => {
    const fetchLogs = vi.fn();
    const scrollIntoView = vi.spyOn(Element.prototype, 'scrollIntoView');
    useDeviceStore.setState({ fetchLogs });
    const first = { from: '2026-07-08T00:00:00Z', to: '2026-07-08T01:00:00Z' };
    const second = { from: '2026-07-09T02:00:00Z', to: '2026-07-09T03:30:00Z' };
    const { rerender } = render(<DeviceLogs deviceId="d1" focusWindow={first} />);
    await userEvent.selectOptions(screen.getByDisplayValue('All Levels'), 'ERROR');

    rerender(<DeviceLogs deviceId="d2" focusWindow={second} />);

    const label = `${new Date(second.from).toLocaleString()} – ${new Date(second.to).toLocaleString()}`;
    expect(screen.getByRole('button', { name: `${label} ✕` })).toHaveAttribute('title', label);
    expect(fetchLogs).toHaveBeenLastCalledWith('d2', {
      level: 'ERROR',
      search: undefined,
      from: second.from,
      to: second.to,
      offset: 0,
      limit: 300,
    });
    expect(scrollIntoView).toHaveBeenLastCalledWith({ behavior: 'smooth', block: 'start' });
  });

  it('uses the current level for range selection, facet selection, and clearing', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-14T19:00:00.000Z'));
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs, logs: sampleLogs });
    render(<DeviceLogs deviceId="d1" />);

    fireEvent.change(screen.getByDisplayValue('All Levels'), { target: { value: 'ERROR' } });
    fireEvent.click(screen.getByRole('button', { name: '1h' }));
    expect(fetchLogs).toHaveBeenLastCalledWith('d1', expect.objectContaining({ level: 'ERROR' }));

    fireEvent.click(screen.getByRole('button', { name: 'WARN 1' }));
    expect(fetchLogs).toHaveBeenLastCalledWith('d1', expect.objectContaining({
      level: 'WARN',
      from: '2026-07-14T18:00:00.000Z',
      to: '2026-07-14T19:00:00.000Z',
    }));

    fireEvent.click(screen.getByRole('button', { name: /✕/ }));
    expect(fetchLogs).toHaveBeenLastCalledWith('d1', expect.objectContaining({
      level: 'WARN', from: undefined, to: undefined,
    }));
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
