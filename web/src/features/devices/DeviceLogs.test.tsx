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

type Logs = typeof sampleLogs | { entries: { timestamp: string; level: string; target: string; message: string }[]; total: number; has_more: boolean } | null;

/** Set the agent pane's slice (the DeviceLogs wrapper renders `source=agent`). */
function setAgentLogs(logs: Logs, loading = false) {
  useDeviceStore.setState({
    logs: { agent: logs, system: null },
    logsLoading: { agent: loading, system: false },
  });
}

describe('DeviceLogs (Agent pane over LogExplorer)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      logs: { agent: null, system: null },
      logsLoading: { agent: false, system: false },
      fetchLogs: vi.fn(),
    });
  });

  afterEach(() => { vi.useRealTimers(); });

  it('renders the Agent Logs header without a Fetch Logs button', () => {
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Agent Logs')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Fetch Logs' })).toBeNull();
  });

  it('does not render a unit filter dropdown (agent pane has no units)', () => {
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.queryByLabelText('Unit')).toBeNull();
  });

  it('displays log entries with level color coding', () => {
    setAgentLogs(sampleLogs);
    render(<DeviceLogs deviceId="d1" />);

    expect(screen.getByText('agent started')).toBeInTheDocument();
    expect(screen.getByText('slow heartbeat')).toBeInTheDocument();
    expect(screen.getByText('connection lost')).toBeInTheDocument();

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
    setAgentLogs(sampleLogs);
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Showing 1-3 of 3')).toBeInTheDocument();
  });

  it('shows a loading indicator while fetching', () => {
    setAgentLogs(null, true);
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Fetching…')).toBeInTheDocument();
  });

  it('shows empty state when no logs', () => {
    setAgentLogs({ entries: [], total: 0, has_more: false });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('No logs available')).toBeInTheDocument();
  });

  it('refetches immediately when the level dropdown changes', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });

    render(<DeviceLogs deviceId="d1" />);
    await user.selectOptions(screen.getByDisplayValue('All Levels'), 'ERROR');

    expect(fetchLogs).toHaveBeenCalledWith('agent', 'd1', expect.objectContaining({ level: 'ERROR', offset: 0, limit: 300 }));
  });

  it('fetches on Enter in the search box', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });

    render(<DeviceLogs deviceId="d1" />);
    await user.type(screen.getByPlaceholderText('Search keyword...'), 'timeout{Enter}');

    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd1', expect.objectContaining({ search: 'timeout', offset: 0 }));
  });

  it('enables the Next-page arrow when has_more is true', () => {
    setAgentLogs({ ...sampleLogs, has_more: true, total: 150 });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByRole('button', { name: 'Next page' })).toBeEnabled();
  });

  it('Next page advances the offset by one page (offset paging, not append)', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setAgentLogs({ ...sampleLogs, has_more: true, total: 600 });
    render(<DeviceLogs deviceId="d1" />);

    await user.click(screen.getByRole('button', { name: 'Next page' }));
    expect(fetchLogs).toHaveBeenCalledWith('agent', 'd1', expect.objectContaining({
      offset: 300,
      limit: 300,
    }));

    await user.click(screen.getByRole('button', { name: 'Next page' }));
    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd1', expect.objectContaining({ offset: 600 }));
  });

  it('Previous page is disabled on the first page and enabled after paging forward', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setAgentLogs({ ...sampleLogs, has_more: true, total: 600 });
    render(<DeviceLogs deviceId="d1" />);

    // offset 0 → cannot go back
    expect(screen.getByRole('button', { name: 'Previous page' })).toBeDisabled();

    await user.click(screen.getByRole('button', { name: 'Next page' }));
    // now on page 2 → prev is enabled and steps back one page
    expect(screen.getByRole('button', { name: 'Previous page' })).toBeEnabled();
    await user.click(screen.getByRole('button', { name: 'Previous page' }));
    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd1', expect.objectContaining({ offset: 0 }));
  });

  it('paginator arrows use the Restart-Agent yellow palette', () => {
    setAgentLogs({ ...sampleLogs, has_more: true, total: 600 });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByRole('button', { name: 'Next page' })).toHaveClass('bg-yellow-600', 'hover:bg-yellow-700');
  });

  it('collapse caret hides the entries + pager but keeps the filter bar', async () => {
    const user = userEvent.setup();
    setAgentLogs({ ...sampleLogs, has_more: true, total: 600 });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('agent started')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /collapse|expand/i }));
    // entries + pager gone…
    expect(screen.queryByText('agent started')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
    // …filter bar stays.
    expect(screen.getByPlaceholderText('Search keyword...')).toBeInTheDocument();

    // Toggling again restores the entries.
    await user.click(screen.getByRole('button', { name: /collapse|expand/i }));
    expect(screen.getByText('agent started')).toBeInTheDocument();
  });

  it('the entries region is drag-resizable (native resize handle)', () => {
    setAgentLogs(sampleLogs);
    const { container } = render(<DeviceLogs deviceId="d1" />);
    expect(container.querySelector('.resize-y')).not.toBeNull();
  });

  it('passes level and search filters to fetchLogs via a window button', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<DeviceLogs deviceId="d1" />);

    await user.type(screen.getByPlaceholderText('Search keyword...'), 'timeout');
    await user.selectOptions(screen.getByDisplayValue('All Levels'), 'ERROR');

    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd1', expect.objectContaining({
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

    await user.click(screen.getByRole('button', { name: '1h' }));

    expect(fetchLogs).toHaveBeenCalledTimes(1);
    const [, , args] = fetchLogs.mock.calls[0]!;
    expect(args.level).toBeUndefined();
    expect(args.search).toBeUndefined();
  });

  it('agent pane never sends a unit filter', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<DeviceLogs deviceId="d1" />);
    await user.click(screen.getByRole('button', { name: '1h' }));
    const [, , args] = fetchLogs.mock.calls[0]!;
    expect(args.unit).toBeUndefined();
  });

  it('level entry uses red color class for ERROR rows', () => {
    setAgentLogs(sampleLogs);
    render(<DeviceLogs deviceId="d1" />);
    const errorCell = screen.getAllByText(/ERROR/).find((el) => el.tagName === 'TD');
    expect(errorCell?.className).toContain('text-red-400');
  });

  it('level entry uses gray-400 fallback class for unknown level', () => {
    setAgentLogs({ ...sampleLogs, entries: [
      { timestamp: 't1', level: 'UNKNOWN', target: 'x', message: 'weird' },
    ] });
    render(<DeviceLogs deviceId="d1" />);
    const td = screen.getAllByText(/UNKNOWN/).find((el) => el.tagName === 'TD');
    expect(td?.className).toContain('text-gray-400');
  });

  it('a time-range chip fetches a bounded window', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    render(<DeviceLogs deviceId="d1" />);
    await user.click(screen.getByRole('button', { name: '1h' }));
    const [, , args] = fetchLogs.mock.calls[0]!;
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
    const lastArgs = fetchLogs.mock.calls.at(-1)![2];
    expect(lastArgs.from).toBeUndefined();
    expect(lastArgs.to).toBeUndefined();
  });

  it('clicking a level facet chip quick-filters that level', async () => {
    const user = userEvent.setup();
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    setAgentLogs(sampleLogs);
    render(<DeviceLogs deviceId="d1" />);
    await user.click(screen.getByRole('button', { name: /ERROR 1/ }));
    expect(fetchLogs).toHaveBeenCalledWith('agent', 'd1', expect.objectContaining({ level: 'ERROR', offset: 0 }));
  });

  it('orders facets by exact count and renders their inactive colors', () => {
    setAgentLogs({
      entries: [
        ...sampleLogs.entries,
        { ...sampleLogs.entries[1]!, timestamp: 'w2' },
        { ...sampleLogs.entries[1]!, timestamp: 'w3' },
        { ...sampleLogs.entries[0]!, timestamp: 'i2' },
      ],
      total: 6,
      has_more: false,
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
    useDeviceStore.setState({ fetchLogs });
    setAgentLogs(sampleLogs);
    render(<DeviceLogs deviceId="d1" />);
    const errorFacet = screen.getByRole('button', { name: 'ERROR 1' });

    await userEvent.click(errorFacet);
    expect(errorFacet).toHaveClass('bg-blue-600', 'text-white', 'text-red-400');
    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd1', expect.objectContaining({ level: 'ERROR' }));

    await userEvent.click(errorFacet);
    expect(errorFacet).toHaveClass('bg-gray-700', 'hover:bg-gray-600', 'text-red-400');
    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd1', expect.objectContaining({ level: undefined }));
  });

  it('omits the facet row when the page has no levels', () => {
    setAgentLogs({ entries: [], total: 0, has_more: false });
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

    expect(fetchLogs).toHaveBeenCalledExactlyOnceWith('agent', 'd1', {
      level: undefined,
      search: undefined,
      unit: undefined,
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
    expect(fetchLogs).toHaveBeenCalledWith('agent', 'd1', expect.objectContaining({
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
    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd2', {
      level: 'ERROR',
      search: undefined,
      unit: undefined,
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
    useDeviceStore.setState({ fetchLogs });
    setAgentLogs(sampleLogs);
    render(<DeviceLogs deviceId="d1" />);

    fireEvent.change(screen.getByDisplayValue('All Levels'), { target: { value: 'ERROR' } });
    fireEvent.click(screen.getByRole('button', { name: '1h' }));
    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd1', expect.objectContaining({ level: 'ERROR' }));

    fireEvent.click(screen.getByRole('button', { name: 'WARN 1' }));
    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd1', expect.objectContaining({
      level: 'WARN',
      from: '2026-07-14T18:00:00.000Z',
      to: '2026-07-14T19:00:00.000Z',
    }));

    fireEvent.click(screen.getByRole('button', { name: /✕/ }));
    expect(fetchLogs).toHaveBeenLastCalledWith('agent', 'd1', expect.objectContaining({
      level: 'WARN', from: undefined, to: undefined,
    }));
  });

  it('Showing total clamps to logs.total (Math.min branch)', () => {
    setAgentLogs({ ...sampleLogs, total: 5 });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByText('Showing 1-3 of 5')).toBeInTheDocument();
  });

  it('Next-page arrow disabled when has_more is false', () => {
    setAgentLogs({ ...sampleLogs, has_more: false });
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled();
  });

  it('pager arrows are disabled while another fetch is in flight', () => {
    setAgentLogs({ ...sampleLogs, has_more: true, total: 100 }, true);
    render(<DeviceLogs deviceId="d1" />);
    expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled();
    // prev is also disabled (offset 0), so assert the in-flight guard via next.
  });

  it('level dropdown contains all five named levels plus "All Levels"', () => {
    render(<DeviceLogs deviceId="d1" />);
    const select = screen.getByDisplayValue('All Levels') as HTMLSelectElement;
    const labels = Array.from(select.options).map((o) => o.textContent);
    expect(labels).toEqual(['All Levels', 'TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR']);
  });
});
