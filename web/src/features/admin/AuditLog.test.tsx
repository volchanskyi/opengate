import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAdminStore } from '../../state/admin-store';
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

  it('user_id is rendered as 8-char prefix', () => {
    render(<AuditLog />);
    // 'u1-abcd-1234-5678-0000'.slice(0, 8) === 'u1-abcd-' — kills `slice(0, 8)`
    // → `slice()` (no args) mutant which would render the full id.
    expect(screen.getByText('u1-abcd-')).toBeInTheDocument();
    expect(screen.getByText('u2-abcd-')).toBeInTheDocument();
  });
});
