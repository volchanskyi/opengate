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
    expect(fetchFn).toHaveBeenCalledWith({ limit: 50, offset: 0 });
  });

  it('typing in filter input adds action key to fetchAuditEvents call', async () => {
    const fetchFn = vi.fn();
    useAdminStore.setState({ fetchAuditEvents: fetchFn });
    render(<AuditLog />);
    fetchFn.mockClear();

    const input = screen.getByPlaceholderText('Filter by action...');
    await userEvent.type(input, 'login');

    const lastCall = fetchFn.mock.calls.at(-1)?.[0];
    expect(lastCall).toMatchObject({ limit: 50, offset: 0, action: 'login' });
  });

  it('Next button advances offset by limit', async () => {
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
    const lastCall = fetchFn.mock.calls.at(-1)?.[0];
    expect(lastCall).toMatchObject({ limit: 50, offset: 50 });
  });

  it('changing the action filter returns to the first page', async () => {
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
    await userEvent.click(screen.getByText('Next'));
    fetchFn.mockClear();

    await userEvent.type(screen.getByPlaceholderText('Filter by action...'), 'x');

    expect(fetchFn.mock.calls.at(-1)?.[0]).toMatchObject({ offset: 0, action: 'x' });
  });

  it('Next button disabled when fewer events than limit', () => {
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

    expect(screen.getByText('act-0')).toBeInTheDocument();
    expect(screen.queryByText('act-499')).toBeNull();
    const actionCells = screen.queryAllByText(/^act-\d+$/);
    expect(actionCells.length).toBeGreaterThan(0);
    expect(actionCells.length).toBeLessThan(500);
  });

  it('user_id is rendered as 8-char prefix', () => {
    render(<AuditLog />);
    expect(screen.getByText('u1-abcd-')).toBeInTheDocument();
    expect(screen.getByText('u2-abcd-')).toBeInTheDocument();
  });

  it('renders no spacer rows when every event fits the viewport', () => {
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
    expect(spacers).toHaveLength(1);

    const height = Number.parseFloat((spacers[0] as HTMLElement).style.height);
    expect(height).toBeGreaterThan(0);
    expect(height).toBeLessThan(count * 41);
  });
});
