import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useUpdateStore } from '../../state/update-store';
import { useAuthStore } from '../../state/auth-store';
import { AgentSetupPage } from './AgentSetupPage';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    POST: vi.fn().mockResolvedValue({ data: {}, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

const adminUser = {
  id: 'u1',
  email: 'admin@test.com',
  display_name: 'Admin',
  is_admin: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const regularUser = {
  id: 'u2',
  email: 'user@test.com',
  display_name: 'User',
  is_admin: false,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const fakeManifest = {
  version: '1.0.0',
  os: 'linux',
  arch: 'amd64',
  url: 'https://github.com/example/releases/download/v1.0.0/mesh-agent-linux-amd64',
  sha256: 'abc123',
  signature: 'sig',
  created_at: '2024-01-01T00:00:00Z',
};

const fakeToken = {
  id: 't1',
  token: 'abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890',
  label: 'Quick setup',
  created_by: 'u1',
  max_uses: 0,
  use_count: 0,
  expires_at: '2099-01-01T00:00:00Z',
  created_at: '2024-01-01T00:00:00Z',
};

const expiredToken = {
  id: 't2',
  token: 'expired0000000000000000000000000000000000000000000000000000000000',
  label: 'Old token',
  created_by: 'u1',
  max_uses: 0,
  use_count: 3,
  expires_at: '2020-01-01T00:00:00Z',
  created_at: '2019-01-01T00:00:00Z',
};

const exhaustedToken = {
  id: 't3',
  token: 'exhausted000000000000000000000000000000000000000000000000000000',
  label: 'Limited token',
  created_by: 'u1',
  max_uses: 5,
  use_count: 5,
  expires_at: '2099-01-01T00:00:00Z',
  created_at: '2024-01-01T00:00:00Z',
};

// No-op fetch functions to prevent useEffect from overwriting test state.
const noopFetch = vi.fn();
const noopCreate = vi.fn();
const noopDelete = vi.fn();

describe('AgentSetupPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      token: 'valid-token',
      user: adminUser,
      isLoading: false,
      error: null,
    });
    useUpdateStore.setState({
      manifests: [fakeManifest],
      enrollmentTokens: [fakeToken],
      isLoading: false,
      error: null,
      fetchManifests: noopFetch,
      fetchEnrollmentTokens: noopFetch,
      createEnrollmentToken: noopCreate,
      deleteEnrollmentToken: noopDelete,
    });
  });

  it('renders page title', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('shows one-liner with enrollment token', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText(/curl -sL/)).toBeInTheDocument();
    expect(screen.getByText(/sudo bash -s --/)).toBeInTheDocument();
    expect(screen.getAllByText('Copy').length).toBeGreaterThanOrEqual(1);
  });

  it('shows create token prompt for admin without tokens', () => {
    useUpdateStore.setState({ enrollmentTokens: [] });
    render(<AgentSetupPage />);
    expect(screen.getByText(/Create an enrollment token below/)).toBeInTheDocument();
  });

  it('shows message for non-admin without tokens', () => {
    useAuthStore.setState({ user: regularUser });
    useUpdateStore.setState({ enrollmentTokens: [] });
    render(<AgentSetupPage />);
    expect(screen.getByText(/Ask your administrator/)).toBeInTheDocument();
  });

  it('shows what happens next section', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText('What happens next')).toBeInTheDocument();
  });

  it('shows enrollment tokens section for admin', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText('Enrollment Tokens')).toBeInTheDocument();
    expect(screen.getByText(fakeToken.token)).toBeInTheDocument();
  });

  it('hides enrollment tokens section for non-admin', () => {
    useAuthStore.setState({ user: regularUser });
    render(<AgentSetupPage />);
    expect(screen.queryByText('Enrollment Tokens')).not.toBeInTheDocument();
  });

  it('shows active badge for valid token', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText('Active')).toBeInTheDocument();
  });

  it('shows expired badge for expired token', () => {
    useUpdateStore.setState({ enrollmentTokens: [expiredToken] });
    render(<AgentSetupPage />);
    expect(screen.getByText('Expired')).toBeInTheDocument();
  });

  it('shows exhausted badge for used-up token', () => {
    useUpdateStore.setState({ enrollmentTokens: [exhaustedToken] });
    render(<AgentSetupPage />);
    expect(screen.getByText('Exhausted')).toBeInTheDocument();
  });

  it('shows token usage count and expiry', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText(/Uses: 0/)).toBeInTheDocument();
    expect(screen.getByText(/unlimited/)).toBeInTheDocument();
  });

  it('shows new token form when New Token is clicked', async () => {
    render(<AgentSetupPage />);
    await userEvent.click(screen.getByText('New Token'));
    expect(screen.getByLabelText('Label')).toBeInTheDocument();
    expect(screen.getByLabelText(/Max uses/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Expires in/)).toBeInTheDocument();
  });

  it('calls createEnrollmentToken on form submit', async () => {
    render(<AgentSetupPage />);
    await userEvent.click(screen.getByText('New Token'));
    await userEvent.click(screen.getByText('Create'));
    expect(noopCreate).toHaveBeenCalledWith({
      label: 'Quick setup',
      max_uses: 0,
      expires_in_hours: 24,
    });
  });

  it('passes the typed-in label, max_uses, and expires_in_hours through to createEnrollmentToken', async () => {
    render(<AgentSetupPage />);
    await userEvent.click(screen.getByText('New Token'));

    const labelInput = screen.getByLabelText('Label') as HTMLInputElement;
    await userEvent.type(labelInput, 'My Label');

    // For type=number inputs, use fireEvent.change to set the value directly
    // (userEvent.clear leaves behind whatever was prefilled by HTML number stepping).
    const maxUsesInput = screen.getByLabelText(/Max uses/) as HTMLInputElement;
    fireEvent.change(maxUsesInput, { target: { value: '5' } });

    const expiresInput = screen.getByLabelText(/Expires in/) as HTMLInputElement;
    fireEvent.change(expiresInput, { target: { value: '48' } });

    await userEvent.click(screen.getByText('Create'));

    // Pin all three numeric / string fields — kills:
    // - ArrowFunction `() => undefined` mutants on the onChange handlers
    //   (they would prevent any of these fields from updating).
    // - LogicalOperator `||` → `&&` mutants on parseInt fallbacks.
    expect(noopCreate).toHaveBeenCalledWith({
      label: 'My Label',
      max_uses: 5,
      expires_in_hours: 48,
    });
  });

  it('hides token form after successful create (and no orphan label remains)', async () => {
    render(<AgentSetupPage />);
    await userEvent.click(screen.getByText('New Token'));
    expect(screen.getByLabelText('Label')).toBeInTheDocument();

    await userEvent.click(screen.getByText('Create'));

    // The form must hide (showTokenForm=false) and label/maxUses/expires must
    // reset — kills `setShowTokenForm(false)` → `setShowTokenForm(true)` mutant
    // and `setTokenLabel('')` → `setTokenLabel("Stryker was here!")` mutant.
    expect(screen.queryByLabelText('Label')).not.toBeInTheDocument();

    // Re-open the form and confirm fields are reset to defaults (empty / 0 / 24).
    await userEvent.click(screen.getByText('New Token'));
    expect((screen.getByLabelText('Label') as HTMLInputElement).value).toBe('');
    expect((screen.getByLabelText(/Max uses/) as HTMLInputElement).value).toBe('0');
    expect((screen.getByLabelText(/Expires in/) as HTMLInputElement).value).toBe('24');
  });

  it('renders Untitled when label is empty (kills `t.label || "Untitled"` survival)', () => {
    useUpdateStore.setState({
      enrollmentTokens: [{ ...fakeToken, label: '' }],
    });
    render(<AgentSetupPage />);
    expect(screen.getByText('Untitled')).toBeInTheDocument();
  });

  it('does not show Active badge for an inactive (expired || exhausted) token', () => {
    useUpdateStore.setState({ enrollmentTokens: [expiredToken] });
    render(<AgentSetupPage />);
    // Pin: only expired tokens are inactive — kills `expired || exhausted` →
    // `expired && exhausted` mutant (which would render Active for an expired
    // but not exhausted token).
    expect(screen.queryByText('Active')).toBeNull();
  });

  it('calls deleteEnrollmentToken when delete is clicked', async () => {
    render(<AgentSetupPage />);
    await userEvent.click(screen.getByText('Delete'));
    expect(noopDelete).toHaveBeenCalledWith(fakeToken.id);
  });

  it('shows no tokens message when list is empty', () => {
    useUpdateStore.setState({ enrollmentTokens: [] });
    render(<AgentSetupPage />);
    expect(screen.getByText('No enrollment tokens created yet.')).toBeInTheDocument();
  });

  it('hides new token form on cancel', async () => {
    render(<AgentSetupPage />);
    await userEvent.click(screen.getByText('New Token'));
    expect(screen.getByLabelText('Label')).toBeInTheDocument();
    await userEvent.click(screen.getByText('Cancel'));
    expect(screen.queryByLabelText('Label')).not.toBeInTheDocument();
  });

  it('shows token with copy button', async () => {
    render(<AgentSetupPage />);
    const copyButtons = screen.getAllByText('Copy');
    expect(copyButtons.length).toBeGreaterThanOrEqual(2);
  });

  it('shows loading screen while loading with no tokens yet', () => {
    useUpdateStore.setState({ isLoading: true, enrollmentTokens: [] });
    render(<AgentSetupPage />);
    expect(screen.getByText('Loading...')).toBeInTheDocument();
    expect(screen.queryByText('Add Device')).toBeNull();
  });

  it('skips loading screen when isLoading is true but tokens are present', () => {
    useUpdateStore.setState({ isLoading: true, enrollmentTokens: [fakeToken] });
    render(<AgentSetupPage />);
    expect(screen.queryByText('Loading...')).toBeNull();
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('fetchEnrollmentTokens is called on admin mount but skipped for non-admin', () => {
    // Admin
    const adminFetch = vi.fn();
    useAuthStore.setState({ user: adminUser, token: 't' });
    useUpdateStore.setState({ fetchEnrollmentTokens: adminFetch, enrollmentTokens: [] });
    const { unmount } = render(<AgentSetupPage />);
    expect(adminFetch).toHaveBeenCalled();
    unmount();

    // Non-admin
    const userFetch = vi.fn();
    useAuthStore.setState({ user: regularUser, token: 't' });
    useUpdateStore.setState({ fetchEnrollmentTokens: userFetch, enrollmentTokens: [] });
    render(<AgentSetupPage />);
    expect(userFetch).not.toHaveBeenCalled();
  });

  it('Copy button label flips to "Copied!" after click', async () => {
    // Mock clipboard write to resolve immediately.
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    render(<AgentSetupPage />);
    // The "install" command Copy button.
    const copyBtn = screen.getAllByText('Copy')[0]!;
    await userEvent.click(copyBtn);
    // Allow microtask queue to flush.
    await Promise.resolve();
    expect(await screen.findByText('Copied!')).toBeInTheDocument();
  });

  it('install command contains the active token and the right URL pattern', () => {
    render(<AgentSetupPage />);
    const codeText = screen.getByText(/curl -sL/).textContent ?? '';
    expect(codeText).toContain(fakeToken.token);
    expect(codeText).toMatch(/sudo bash -s --/);
    expect(codeText).toContain('/api/v1/server/install.sh');
  });

  it('installCommand is null when no active token exists (no install snippet rendered)', () => {
    useUpdateStore.setState({ enrollmentTokens: [expiredToken] });
    render(<AgentSetupPage />);
    expect(screen.queryByText(/curl -sL/)).toBeNull();
  });
});
