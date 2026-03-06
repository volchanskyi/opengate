import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useConnectionStore } from '../../state/connection-store';
import { RemoteDesktopView } from './RemoteDesktopView';

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
