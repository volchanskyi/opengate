import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { useConnectionStore } from '../../state/connection-store';
import { useAuthStore } from '../../state/auth-store';
import { SessionView } from './SessionView';

// Mock child views to avoid heavy dependencies (xterm, canvas, etc.)
vi.mock('../remote-desktop/RemoteDesktopView', () => ({
  RemoteDesktopView: () => <div data-testid="desktop-view">Desktop</div>,
}));
vi.mock('../terminal/TerminalView', () => ({
  TerminalView: () => <div data-testid="terminal-view">Terminal</div>,
}));
vi.mock('../file-manager/FileManagerView', () => ({
  FileManagerView: () => <div data-testid="files-view">Files</div>,
}));
vi.mock('../messenger/MessengerView', () => ({
  MessengerView: () => <div data-testid="messenger-view">Chat</div>,
}));
vi.mock('./SessionToolbar', () => ({
  SessionToolbar: () => <div data-testid="toolbar">Toolbar</div>,
}));
vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

function renderWithRouter(capabilities?: string[]) {
  const state = { relayUrl: 'ws://localhost/relay/tok', capabilities };
  return render(
    <MemoryRouter initialEntries={[{ pathname: '/sessions/tok', state }]}>
      <Routes>
        <Route path="/sessions/:token" element={<SessionView />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('SessionView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useConnectionStore.setState({
      state: 'connected',
      transport: null,
      error: null,
    });
    useAuthStore.setState({ token: 'jwt' });
  });

  it('shows all tabs when capabilities include RemoteDesktop', () => {
    renderWithRouter(['RemoteDesktop', 'Terminal', 'FileManager']);
    expect(screen.getByRole('tab', { name: 'Desktop' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Terminal' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Files' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Chat' })).toBeInTheDocument();
  });

  it('hides Desktop and Chat tabs on headless device', () => {
    renderWithRouter(['Terminal', 'FileManager']);
    expect(screen.queryByRole('tab', { name: 'Desktop' })).not.toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Chat' })).not.toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Terminal' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Files' })).toBeInTheDocument();
  });

  it('shows all tabs when capabilities are undefined (legacy agent)', () => {
    renderWithRouter(undefined);
    expect(screen.getByRole('tab', { name: 'Desktop' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Terminal' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Files' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Chat' })).toBeInTheDocument();
  });

  it('defaults to Terminal tab', () => {
    renderWithRouter(['Terminal', 'FileManager']);
    expect(screen.getByRole('tab', { name: 'Terminal' })).toHaveAttribute('aria-selected', 'true');
  });
});
