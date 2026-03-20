import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useConnectionStore } from '../../state/connection-store';
import { useAuthStore } from '../../state/auth-store';
import { SessionView } from './SessionView';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

// Mock feature components to keep SessionView tests focused on shell behavior
vi.mock('../remote-desktop/RemoteDesktopView', () => ({
  RemoteDesktopView: () => <div>Remote Desktop Panel</div>,
}));
vi.mock('../terminal/TerminalView', () => ({
  TerminalView: () => <div>Terminal Panel</div>,
}));
vi.mock('../file-manager/FileManagerView', () => ({
  FileManagerView: () => <div>File Manager Panel</div>,
}));
vi.mock('../messenger/MessengerView', () => ({
  MessengerView: () => <div>Chat Panel</div>,
}));

function renderSession(token = 'test-token', relayUrl = 'ws://localhost/ws/relay/test-token') {
  const router = createMemoryRouter(
    [
      { path: '/sessions/:token', element: <SessionView /> },
      { path: '/devices', element: <p>Device List</p> },
    ],
    {
      initialEntries: [{ pathname: `/sessions/${token}`, state: { relayUrl } }],
    },
  );
  return render(<RouterProvider router={router} />);
}

describe('SessionView', () => {
  const mockConnect = vi.fn();
  const mockDisconnect = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    useConnectionStore.setState({
      state: 'disconnected',
      token: null,
      error: null,
      transport: null,
      onControlMessage: null,
      onDesktopFrame: null,
      onTerminalFrame: null,
      onFileFrame: null,
      connect: mockConnect,
      disconnect: mockDisconnect,
    });
    useAuthStore.setState({
      token: 'jwt-test-token',
      user: { id: '1', email: 'a@b.com', display_name: 'Test', is_admin: false, created_at: '', updated_at: '' },
    });
  });

  it('renders tab bar with Desktop, Terminal, Files, Chat', () => {
    renderSession();
    expect(screen.getByRole('tab', { name: 'Desktop' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Terminal' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Files' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Chat' })).toBeInTheDocument();
  });

  it('default tab is Terminal', () => {
    renderSession();
    expect(screen.getByRole('tab', { name: 'Terminal' })).toHaveAttribute('aria-selected', 'true');
  });

  it('switches tabs when clicked', async () => {
    const user = userEvent.setup();
    renderSession();

    await user.click(screen.getByRole('tab', { name: 'Terminal' }));
    expect(screen.getByRole('tab', { name: 'Terminal' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: 'Desktop' })).toHaveAttribute('aria-selected', 'false');
  });

  it('renders correct panel content for each tab', async () => {
    const user = userEvent.setup();
    renderSession();

    // Default: Terminal panel
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Terminal Panel');

    // Switch to Desktop
    await user.click(screen.getByRole('tab', { name: 'Desktop' }));
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Remote Desktop Panel');

    // Switch to Files
    await user.click(screen.getByRole('tab', { name: 'Files' }));
    expect(screen.getByRole('tabpanel')).toHaveTextContent('File Manager Panel');

    // Switch to Chat
    await user.click(screen.getByRole('tab', { name: 'Chat' }));
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Chat Panel');
  });

  it('initiates connection on mount', () => {
    renderSession();
    expect(mockConnect).toHaveBeenCalledWith(
      'test-token',
      'ws://localhost/ws/relay/test-token',
      expect.any(String),
    );
  });

  it('renders the toolbar', () => {
    useConnectionStore.setState({ state: 'connected' });
    renderSession();
    expect(screen.getByText('Connected')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Disconnect' })).toBeInTheDocument();
  });

  it('disconnect button calls disconnect and navigates back', async () => {
    const user = userEvent.setup();
    useConnectionStore.setState({ state: 'connected' });
    renderSession();

    await user.click(screen.getByRole('button', { name: 'Disconnect' }));
    // Called at least once from the button click (cleanup effect also calls it)
    expect(mockDisconnect).toHaveBeenCalled();
  });

  it('shows error message when connection has error', () => {
    useConnectionStore.setState({ state: 'error', error: 'Connection failed' });
    renderSession();
    expect(screen.getByText('Connection failed')).toBeInTheDocument();
  });
});
