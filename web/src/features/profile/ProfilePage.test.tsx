import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAuthStore } from '../../state/auth-store';
import { useToastStore } from '../../lib/feedback/toast-store';
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

  it('shows "Profile updated" success toast and refreshes /me after a successful PATCH', async () => {
    const addToast = vi.fn();
    const fetchMeFn = vi.fn().mockResolvedValue(undefined);
    useToastStore.setState({ addToast });
    useAuthStore.setState({ fetchMe: fetchMeFn });
    render(<ProfilePage />);
    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => {
      expect(addToast).toHaveBeenCalledWith('Profile updated', 'success');
    });
    expect(fetchMeFn).toHaveBeenCalled();
  });

  it('Save button label flips to "Saving..." while in-flight and disables', async () => {
    const { api } = await import('../../lib/api');
    let resolve: (v: { data: unknown; error: undefined }) => void = () => undefined;
    vi.mocked(api.PATCH).mockReturnValueOnce(new Promise((r) => { resolve = r; }) as never);
    render(<ProfilePage />);
    fireEvent.click(screen.getByText('Save'));
    const btn = await screen.findByText('Saving...');
    expect((btn as HTMLButtonElement).disabled).toBe(true);
    resolve({ data: {}, error: undefined });
  });

  it('PATCH receives the current display_name from the input', async () => {
    const { api } = await import('../../lib/api');
    render(<ProfilePage />);
    const input = screen.getByDisplayValue('Test User');
    fireEvent.change(input, { target: { value: 'Edited Name' } });
    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => {
      expect(api.PATCH).toHaveBeenCalledWith('/api/v1/users/{id}', {
        params: { path: { id: 'u1' } },
        body: { display_name: 'Edited Name' },
      });
    });
  });

  it('PATCH initially sends the existing display_name when nothing edited', async () => {
    const { api } = await import('../../lib/api');
    render(<ProfilePage />);
    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => {
      expect(api.PATCH).toHaveBeenCalledWith('/api/v1/users/{id}', {
        params: { path: { id: 'u1' } },
        body: { display_name: 'Test User' },
      });
    });
  });

  it('falls back to empty display_name when user has no display_name set', () => {
    useAuthStore.setState({
      user: { id: 'u1', email: 'x@y.z', display_name: '', is_admin: false, created_at: '2026-01-01T00:00:00Z', updated_at: '' },
      fetchMe: vi.fn(),
    });
    render(<ProfilePage />);
    const input = document.querySelector('input[type="text"]') as HTMLInputElement;
    expect(input.value).toBe('');
  });
});
