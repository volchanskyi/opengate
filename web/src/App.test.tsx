import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import App from './App';
import { useAuthStore } from './state/auth-store';

vi.mock('./lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn(),
  },
}));

describe('App', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useAuthStore.setState({
      token: null,
      user: null,
      isLoading: false,
      hydrated: false,
      error: null,
    });
  });

  it('renders without crashing and redirects to login', async () => {
    render(<App />);
    // Unauthenticated users get redirected to login
    expect(await screen.findByRole('heading', { name: 'Login' })).toBeInTheDocument();
  });
});
