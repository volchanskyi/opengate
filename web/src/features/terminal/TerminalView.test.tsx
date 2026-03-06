import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useConnectionStore } from '../../state/connection-store';
import { TerminalView } from './TerminalView';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

// Mock xterm — jsdom doesn't support real terminal rendering
const mockWrite = vi.fn();
const mockDispose = vi.fn();
const mockOnData = vi.fn();
const mockOnResize = vi.fn();
const mockOpen = vi.fn();
const mockFit = vi.fn();

vi.mock('@xterm/xterm', () => ({
  Terminal: vi.fn().mockImplementation(() => ({
    write: mockWrite,
    dispose: mockDispose,
    onData: mockOnData,
    onResize: mockOnResize,
    open: mockOpen,
    loadAddon: vi.fn(),
  })),
}));

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: vi.fn().mockImplementation(() => ({
    fit: mockFit,
    dispose: vi.fn(),
  })),
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
