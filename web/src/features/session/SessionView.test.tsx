import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter, Route, Routes } from 'react-router';
import { useConnectionStore } from './state/connection-store';
import { useAuthStore } from '../../state/auth-store';
import { identityOf, SIGNED_IN_KEY } from '../../lib/console-session';
import { holdsSessionTab } from './session-tab';
import { SessionView } from './SessionView';
import { useToastStore } from '../../lib/feedback/toast-store';

vi.mock('../remote-desktop', () => ({
  RemoteDesktopView: () => <div data-testid="desktop-view">Desktop</div>,
}));
vi.mock('../terminal', () => ({
  TerminalView: () => <div data-testid="terminal-view">Terminal</div>,
}));
vi.mock('../file-manager', () => ({
  FileManagerView: () => <div data-testid="files-view">Files</div>,
}));
vi.mock('../messenger', () => ({
  MessengerView: () => <div data-testid="messenger-view">Chat</div>,
}));

const mockGet = vi.fn();
const mockPost = vi.fn();
vi.mock('../../lib/api', () => ({
  api: {
    GET: (...args: unknown[]) => mockGet(...args),
    POST: (...args: unknown[]) => mockPost(...args),
    DELETE: vi.fn(),
  },
}));

const ALICE_USER = {
  id: 'alice',
  email: 'alice@northwind.test',
  display_name: 'Alice',
  is_admin: false,
  created_at: '',
  updated_at: '',
};
const ALICE = identityOf('alice', 'northwind');

const DEVICE = {
  id: 'dev-1',
  hostname: 'FS01',
  organization_id: 'org-contoso',
  capabilities: ['Terminal', 'FileManager'],
};

function answer(path: string, opts?: { capabilities?: string[] }) {
  if (path === '/api/v1/devices/{id}') {
    return { data: { ...DEVICE, capabilities: opts?.capabilities ?? DEVICE.capabilities }, error: undefined, response: { status: 200 } };
  }
  if (path === '/api/v1/organizations/{id}') {
    return { data: { id: 'org-contoso', name: 'Contoso' }, error: undefined, response: { status: 200 } };
  }
  return { data: undefined, error: { error: 'unexpected' }, response: { status: 404 } };
}

function opened() {
  return {
    data: { token: 'tok', relay_url: 'wss://edge.test/ws/relay/tok', ice_servers: [{ urls: ['stun:helper.test:9090'] }] },
    error: undefined,
    response: { status: 201 },
  };
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/devices/dev-1/session']}>
      <Routes>
        <Route path="/devices/:id/session" element={<SessionView />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('SessionView', () => {
  const connect = vi.fn();
  const disconnect = vi.fn();

  beforeEach(() => {
    mockGet.mockReset();
    mockPost.mockReset();
    connect.mockReset();
    disconnect.mockReset();
    mockGet.mockImplementation((path: string) => Promise.resolve(answer(path)));
    mockPost.mockResolvedValue(opened());
    useConnectionStore.setState({ state: 'connected', path: { direct: false }, closedReason: null, transport: null, connect, disconnect });
    useAuthStore.setState({ user: ALICE_USER, tenantId: 'northwind' });
    localStorage.setItem(SIGNED_IN_KEY, ALICE);
    window.name = '';
  });

  const originalName = window.name;

  afterEach(() => {
    localStorage.clear();
    window.name = originalName;
  });

  it('opens a session on the host in the address and joins it with the relay and helpers the server named', async () => {
    renderPage();
    await waitFor(() => expect(connect).toHaveBeenCalledWith('tok', 'wss://edge.test/ws/relay/tok', ['stun:helper.test:9090']));
    expect(mockPost).toHaveBeenCalledWith('/api/v1/sessions', expect.objectContaining({ body: expect.objectContaining({ device_id: 'dev-1' }) }));
  });

  it('names the tab after its host and customer, and carries the host’s tab name', async () => {
    renderPage();
    await waitFor(() => expect(document.title).toBe('FS01 — Contoso'));
    expect(screen.getByText('FS01 — Contoso')).toBeInTheDocument();
    expect(window.name).toBe('opengate-session-dev-1');
  });

  it('reads nothing from the customer chosen in another tab', async () => {
    localStorage.setItem('selectedOrganizationId', 'org-fabrikam');
    renderPage();
    await waitFor(() => expect(document.title).toBe('FS01 — Contoso'));
    expect(screen.queryByRole('combobox', { name: /customer/i })).not.toBeInTheDocument();
  });

  it('shows the tabs the host’s capabilities allow', async () => {
    mockGet.mockImplementation((path: string) =>
      Promise.resolve(answer(path, { capabilities: ['RemoteDesktop', 'Terminal', 'FileManager'] })),
    );
    renderPage();
    expect(await screen.findByRole('tab', { name: 'Desktop' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Chat' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Terminal' })).toHaveAttribute('aria-selected', 'true');
  });

  it('hides Desktop and Chat on a host without a screen', async () => {
    renderPage();
    expect(await screen.findByRole('tab', { name: 'Terminal' })).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Desktop' })).not.toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Chat' })).not.toBeInTheDocument();
  });

  it('keeps every feature running while another is shown, so a download carries on beside the terminal', async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole('tab', { name: 'Files' }));
    expect(screen.getByTestId('files-view')).toBeVisible();
    expect(screen.getByTestId('terminal-view')).not.toBeVisible();

    await user.click(screen.getByRole('tab', { name: 'Terminal' }));
    expect(screen.getByTestId('terminal-view')).toBeVisible();
    expect(screen.getByTestId('files-view')).toBeInTheDocument();
    expect(screen.getByTestId('files-view')).not.toBeVisible();
  });

  it('names the technician holding the host when the server refuses, and joins nothing', async () => {
    mockPost.mockResolvedValue({
      data: undefined,
      error: { error: 'device has an open session', holder_name: 'Alice', started_at: '2026-10-12T14:02:00Z' },
      response: { status: 409 },
    });
    renderPage();
    expect(await screen.findByText(/^Session open — Alice, since \d{1,2}:\d{2}/)).toBeInTheDocument();
    expect(connect).not.toHaveBeenCalled();
  });

  it('says why the relay closed and stops showing the session', async () => {
    renderPage();
    await screen.findByRole('tab', { name: 'Terminal' });
    act(() => useConnectionStore.setState({ closedReason: 'Opened elsewhere' }));
    expect(await screen.findByText('Opened elsewhere')).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Terminal' })).not.toBeInTheDocument();
  });

  it('ends its session when this browser signs out in another tab', async () => {
    renderPage();
    await screen.findByRole('tab', { name: 'Terminal' });
    act(() => {
      localStorage.removeItem(SIGNED_IN_KEY);
      window.dispatchEvent(new StorageEvent('storage', { key: SIGNED_IN_KEY, newValue: null }));
    });
    expect(await screen.findByText('Signed out')).toBeInTheDocument();
    expect(disconnect).toHaveBeenCalled();
  });

  it('ends a session this browser signed out of while it was opening', async () => {
    let reply: (value: ReturnType<typeof opened>) => void = () => {};
    mockPost.mockReturnValue(new Promise((resolve) => { reply = resolve; }));
    renderPage();
    act(() => {
      localStorage.removeItem(SIGNED_IN_KEY);
      window.dispatchEvent(new StorageEvent('storage', { key: SIGNED_IN_KEY, newValue: null }));
    });
    await act(async () => reply(opened()));
    expect(await screen.findByText('Signed out')).toBeInTheDocument();
    expect(disconnect).toHaveBeenCalled();
  });

  it('tells this browser’s other pages it holds the host while the session is open', async () => {
    const { unmount } = renderPage();
    await waitFor(() => expect(holdsSessionTab('dev-1')).toBe(true));
    unmount();
    expect(holdsSessionTab('dev-1')).toBe(false);
    expect(disconnect).toHaveBeenCalled();
  });

  it('shows the messages its features raise, such as a download that failed', async () => {
    renderPage();
    await screen.findByRole('tab', { name: 'Terminal' });
    act(() => useToastStore.getState().addToast("Couldn't download app.log: permission denied", 'error'));
    expect(screen.getByRole('alert')).toHaveTextContent("Couldn't download app.log: permission denied");
    act(() => useToastStore.setState({ toasts: [] }));
  });

  it('ends the session from its toolbar', async () => {
    const user = userEvent.setup();
    const close = vi.spyOn(window, 'close').mockImplementation(() => {});
    renderPage();
    await screen.findByRole('tab', { name: 'Terminal' });
    await user.click(screen.getByRole('button', { name: 'Disconnect' }));
    expect(disconnect).toHaveBeenCalled();
    expect(close).toHaveBeenCalled();
    expect(screen.getByText('Session ended')).toBeInTheDocument();
    close.mockRestore();
  });
});
