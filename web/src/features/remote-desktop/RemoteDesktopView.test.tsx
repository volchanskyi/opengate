import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useConnectionStore } from '../session';
import { useRemoteDesktop } from './use-remote-desktop';
import { RemoteDesktopView } from './RemoteDesktopView';

vi.mock('./use-remote-desktop', () => ({ useRemoteDesktop: vi.fn() }));

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

describe('RemoteDesktopView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useConnectionStore.setState({
      state: 'disconnected',
      transport: null,
    });
  });

  it('renders a canvas element', () => {
    render(<RemoteDesktopView />);
    expect(document.querySelector('canvas')).toBeInTheDocument();
  });

  it('hands its canvas to the hook that paints it', () => {
    render(<RemoteDesktopView />);
    const ref = vi.mocked(useRemoteDesktop).mock.calls.at(-1)![0];
    expect(ref.current).toBe(document.querySelector('canvas'));
  });

  it('shows placeholder text when disconnected', () => {
    render(<RemoteDesktopView />);
    expect(screen.getByText(/waiting for connection/i)).toBeInTheDocument();
  });

  it('hides placeholder when connected', () => {
    useConnectionStore.setState({ state: 'connected' });
    render(<RemoteDesktopView />);
    expect(screen.queryByText(/waiting for connection/i)).not.toBeInTheDocument();
  });
});
