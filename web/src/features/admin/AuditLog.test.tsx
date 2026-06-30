import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAdminStore } from './state/admin-store';
import { AuditLog } from './AuditLog';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    POST: vi.fn(),
  },
}));

const fakeEvents = [
  {
    id: 1,
    user_id: 'u1-abcd-1234-5678-0000',
    action: 'user.login',
    target: 'admin@test.com',
    details: '',
    created_at: '2024-01-01T12:00:00Z',
  },
  {
    id: 2,
    user_id: 'u2-abcd-1234-5678-0000',
    action: 'session.create',
    target: 'device-1',
    details: 'test session',
    created_at: '2024-01-01T13:00:00Z',
  },
];

describe('AuditLog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAdminStore.setState({
      users: [],
      auditEvents: fakeEvents,
      isLoading: false,
      error: null,
    });
  });

  it('renders audit events', () => {
    render(<AuditLog />);
    expect(screen.getByText('user.login')).toBeInTheDocument();
    expect(screen.getByText('session.create')).toBeInTheDocument();
    expect(screen.getByText('admin@test.com')).toBeInTheDocument();
  });

  it('shows loading state', () => {
    useAdminStore.setState({ auditEvents: [], isLoading: true });
    render(<AuditLog />);
    expect(screen.getByText('Loading audit events...')).toBeInTheDocument();
  });

  it('renders filter input', () => {
    render(<AuditLog />);
    expect(screen.getByPlaceholderText('Filter by action...')).toBeInTheDocument();
  });

  it('renders pagination buttons', () => {
    render(<AuditLog />);
    expect(screen.getByText('Previous')).toBeDisabled();
    expect(screen.getByText('Next')).toBeInTheDocument();
  });

  it('makes the virtualized event table keyboard-scrollable', () => {
    render(<AuditLog />);
    const scrollRegion = screen.getByRole('region', { name: 'Audit events' });
    expect(scrollRegion).toHaveAttribute('tabindex', '0');
  });

  it('calls fetchAuditEvents on mount with limit=50, offset=0, no action filter', () => {
    const fetchFn = vi.fn();
    useAdminStore.setState({ fetchAuditEvents: fetchFn });
    render(<AuditLog />);
    // Pin the literal { limit: 50, offset: 0 } — kills mutants on the limit/offset
    // payload and the `actionFilter ? {...} : {}` ternary's false branch.
    expect(fetchFn).toHaveBeenCalledWith({ limit: 50, offset: 0 });
  });

  it('typing in filter input adds action key to fetchAuditEvents call', async () => {
    const fetchFn = vi.fn();
    useAdminStore.setState({ fetchAuditEvents: fetchFn });
    render(<AuditLog />);
    fetchFn.mockClear();

    const input = screen.getByPlaceholderText('Filter by action...');
    await userEvent.type(input, 'login');

    // Last call must include the action filter — kills the
    // `actionFilter ? {...} : {}` ternary's true branch when collapsed to false/{}.
    const lastCall = fetchFn.mock.calls.at(-1)?.[0];
    expect(lastCall).toMatchObject({ limit: 50, offset: 0, action: 'login' });
  });

  it('Next button advances offset by limit', async () => {
    // Provide exactly limit (50) events so Next is enabled.
    const events = Array.from({ length: 50 }, (_, i) => ({
      id: i + 1,
      user_id: 'u' + String(i),
      action: 'a' + String(i),
      target: 't',
      details: '',
      created_at: '2024-01-01T00:00:00Z',
    }));
    const fetchFn = vi.fn();
    useAdminStore.setState({ auditEvents: events, fetchAuditEvents: fetchFn });
    render(<AuditLog />);
    fetchFn.mockClear();

    await userEvent.click(screen.getByText('Next'));
    // Pins offset advancing by exactly 50 — kills `offset + limit` arithmetic mutants.
    const lastCall = fetchFn.mock.calls.at(-1)?.[0];
    expect(lastCall).toMatchObject({ limit: 50, offset: 50 });
  });

  it('Next button disabled when fewer events than limit', () => {
    // 2 events < 50 → Next disabled.
    render(<AuditLog />);
    expect(screen.getByText('Next')).toBeDisabled();
  });

  it('windows a large audit list (renders a subset, not every row)', () => {
    const many = Array.from({ length: 500 }, (_, i) => ({
      id: i + 1,
      user_id: 'user-' + String(i),
      action: 'act-' + String(i),
      target: 't',
      details: '',
      created_at: '2024-01-01T00:00:00Z',
    }));
    useAdminStore.setState({ auditEvents: many });
    render(<AuditLog />);

    // First row is in the rendered window...
    expect(screen.getByText('act-0')).toBeInTheDocument();
    // ...but a far-off row is virtualized away (not in the DOM).
    expect(screen.queryByText('act-499')).toBeNull();
    // Only a windowed subset of the 500 rows is mounted.
    const actionCells = screen.queryAllByText(/^act-\d+$/);
    expect(actionCells.length).toBeGreaterThan(0);
    expect(actionCells.length).toBeLessThan(500);
  });

  it('user_id is rendered as 8-char prefix', () => {
    render(<AuditLog />);
    // 'u1-abcd-1234-5678-0000'.slice(0, 8) === 'u1-abcd-' — kills `slice(0, 8)`
    // → `slice()` (no args) mutant which would render the full id.
    expect(screen.getByText('u1-abcd-')).toBeInTheDocument();
    expect(screen.getByText('u2-abcd-')).toBeInTheDocument();
  });

  it('renders no spacer rows when every event fits the viewport', () => {
    // beforeEach seeds 2 events × 41px ≪ the 800px mocked viewport → all rows fit, so both
    // paddingTop and paddingBottom are 0 and no spacer <tr> is emitted. Asserting their
    // absence kills the `paddingTop > 0` / `paddingBottom > 0` guard mutants that would
    // otherwise always (>= 0, <= 0, true, &&→||) render a spacer cell.
    render(<AuditLog />);
    expect(document.querySelectorAll('td[colspan="5"]')).toHaveLength(0);
  });

  it('reserves only a bottom spacer, sized below the total height, when the list overflows', () => {
    const count = 500;
    const many = Array.from({ length: count }, (_, i) => ({
      id: i + 1,
      user_id: 'u' + String(i),
      action: 'act-' + String(i),
      target: 't',
      details: '',
      created_at: '2024-01-01T00:00:00Z',
    }));
    useAdminStore.setState({ auditEvents: many });
    render(<AuditLog />);

    const spacers = document.querySelectorAll('td[colspan="5"]');
    // Not scrolled → paddingTop is 0 (no top spacer); only the bottom spacer reserves the
    // off-screen height. A second spacer would mean a `paddingTop > 0` mutant fired.
    expect(spacers).toHaveLength(1);

    const height = Number.parseFloat((spacers[0] as HTMLElement).style.height);
    // paddingBottom = getTotalSize() - lastRow.end ∈ (0, totalSize). The `-`→`+`
    // ArithmeticOperator mutant exceeds totalSize (= count × AUDIT_ROW_HEIGHT = 41); the
    // `> 0`→`<= 0`/`false` mutants drop the spacer entirely.
    expect(height).toBeGreaterThan(0);
    expect(height).toBeLessThan(count * 41);
  });
});
