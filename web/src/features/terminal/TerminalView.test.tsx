import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useConnectionStore } from '../session';
import { useTerminal } from './use-terminal';
import { TerminalView } from './TerminalView';

vi.mock('./use-terminal', () => ({ useTerminal: vi.fn() }));

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

describe('TerminalView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useConnectionStore.setState({
      state: 'disconnected',
      transport: null,
    });
  });

  it('renders terminal container div', () => {
    render(<TerminalView />);
    expect(document.querySelector('[data-testid="terminal-container"]')).toBeInTheDocument();
  });

  it('hands its container to the hook that draws the terminal', () => {
    render(<TerminalView />);
    const ref = vi.mocked(useTerminal).mock.calls.at(-1)![0];
    expect(ref.current).toBe(document.querySelector('[data-testid="terminal-container"]'));
  });

  it('shows placeholder when disconnected', () => {
    render(<TerminalView />);
    expect(screen.getByText(/waiting for connection/i)).toBeInTheDocument();
  });

  it('hides placeholder when connected', () => {
    useConnectionStore.setState({ state: 'connected' });
    render(<TerminalView />);
    expect(screen.queryByText(/waiting for connection/i)).not.toBeInTheDocument();
  });
});
