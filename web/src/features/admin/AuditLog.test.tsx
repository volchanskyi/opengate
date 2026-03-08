import { render, screen } from '@testing-library/react';
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
});
