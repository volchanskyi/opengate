import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAuthStore } from '../../state/auth-store';
import { useToastStore } from '../../state/toast-store';
import { ProfilePage } from './ProfilePage';

vi.mock('../../lib/api', () => ({
  api: {
    PATCH: vi.fn().mockResolvedValue({ data: {}, error: undefined }),
  },
}));

vi.mock('../../lib/fire-and-forget', () => ({
  fireAndForget: (p: Promise<unknown>) => { p.catch(() => {}); },
}));

describe('ProfilePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      user: { id: 'u1', email: 'test@example.com', display_name: 'Test User', is_admin: false, created_at: '2026-01-01T00:00:00Z', updated_at: '' },
      fetchMe: vi.fn(),
    });
    useToastStore.setState({ toasts: [], addToast: vi.fn(), removeToast: vi.fn() });
  });

  it('renders user email and display name', () => {
    render(<ProfilePage />);
    expect(screen.getByText('test@example.com')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Test User')).toBeInTheDocument();
  });

  it('returns null when no user', () => {
    useAuthStore.setState({ user: null });
    const { container } = render(<ProfilePage />);
    expect(container.innerHTML).toBe('');
  });

  it('saves profile on form submit', async () => {
    const { api } = await import('../../lib/api');
    render(<ProfilePage />);

    const input = screen.getByDisplayValue('Test User');
    fireEvent.change(input, { target: { value: 'New Name' } });
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      expect(api.PATCH).toHaveBeenCalled();
    });
  });

  it('shows error toast on failure', async () => {
    const { api } = await import('../../lib/api');
    vi.mocked(api.PATCH).mockResolvedValueOnce({ data: undefined, error: { error: 'failed' }, response: {} as Response });
    const addToast = vi.fn();
    useToastStore.setState({ addToast });

    render(<ProfilePage />);
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      expect(addToast).toHaveBeenCalledWith('failed', 'error');
    });
  });
});
