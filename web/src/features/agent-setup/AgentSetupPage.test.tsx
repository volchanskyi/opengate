import { render, screen } from '@testing-library/react';
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
      caPem: null,
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
});
