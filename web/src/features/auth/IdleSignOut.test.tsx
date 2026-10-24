import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { IdleSignOut } from './IdleSignOut';
import { useAuthStore } from '../../state/auth-store';
import { noteRequest } from '../../lib/console-session';

const mockGet = vi.fn();

vi.mock('../../lib/api', () => ({
  api: {
    GET: (...args: unknown[]) => mockGet(...args),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

const USER = { id: '1', email: 'a@b.com', display_name: 'A', is_admin: false, created_at: '', updated_at: '' };
const MINUTE = 60 * 1000;

describe('IdleSignOut', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockGet.mockReset();
    noteRequest();
    useAuthStore.setState({ user: USER, tenantId: 't', idleTimeoutSeconds: 1800 });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('says nothing while the console is in use', () => {
    render(<IdleSignOut />);
    act(() => {
      vi.advanceTimersByTime(27 * MINUTE);
    });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('warns two minutes before the idle sign-out', () => {
    render(<IdleSignOut />);
    act(() => {
      vi.advanceTimersByTime(28 * MINUTE + 1000);
    });
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/signed out in \d+ seconds/);
  });

  it('signs the console out once the idle limit passes', () => {
    render(<IdleSignOut />);
    act(() => {
      vi.advanceTimersByTime(30 * MINUTE + 1000);
    });
    expect(useAuthStore.getState().user).toBeNull();
  });

  it('a request renews the clock, so the warning goes', () => {
    render(<IdleSignOut />);
    act(() => {
      vi.advanceTimersByTime(28 * MINUTE + 1000);
    });
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();

    act(() => {
      noteRequest();
      vi.advanceTimersByTime(1000);
    });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('staying signed in asks the server, which renews the session', async () => {
    mockGet.mockImplementation(async () => {
      noteRequest();
      return { data: {}, error: undefined, response: { status: 200 } };
    });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<IdleSignOut />);
    act(() => {
      vi.advanceTimersByTime(28 * MINUTE + 1000);
    });

    await user.click(screen.getByRole('button', { name: 'Stay signed in' }));

    expect(mockGet).toHaveBeenCalledWith('/api/v1/auth/session');
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });
});
