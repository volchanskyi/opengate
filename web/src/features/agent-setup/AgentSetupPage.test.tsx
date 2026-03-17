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

// No-op fetch functions to prevent useEffect from overwriting test state.
const noopFetch = vi.fn();
const noopCreate = vi.fn();

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
    });
  });

  it('renders page title', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('renders platform selector', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText('Linux x86_64')).toBeInTheDocument();
    expect(screen.getByText('Linux ARM64')).toBeInTheDocument();
  });

  it('shows one-liner with enrollment token', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText(/curl -sL/)).toBeInTheDocument();
    expect(screen.getByText(/sudo bash -s --/)).toBeInTheDocument();
    expect(screen.getByText('Copy')).toBeInTheDocument();
  });

  it('shows install script download link', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText('Download install.sh')).toBeInTheDocument();
  });

  it('shows download link when manifest exists', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText('Download binary')).toBeInTheDocument();
    expect(screen.getByText('1.0.0')).toBeInTheDocument();
  });

  it('shows no binaries message when manifests empty', () => {
    useUpdateStore.setState({ manifests: [] });
    render(<AgentSetupPage />);
    expect(screen.getByText(/No agent binaries published/)).toBeInTheDocument();
  });

  it('switches platform', async () => {
    const armManifest = { ...fakeManifest, arch: 'arm64' };
    useUpdateStore.setState({ manifests: [fakeManifest, armManifest] });
    render(<AgentSetupPage />);

    await userEvent.click(screen.getByText('Linux ARM64'));
    expect(screen.getByText('Download binary')).toBeInTheDocument();
  });

  it('shows create token button for admin without tokens', () => {
    useUpdateStore.setState({ enrollmentTokens: [] });
    render(<AgentSetupPage />);
    expect(screen.getByText('Create Token')).toBeInTheDocument();
  });

  it('shows message for non-admin without tokens', () => {
    useAuthStore.setState({ user: regularUser });
    useUpdateStore.setState({ enrollmentTokens: [] });
    render(<AgentSetupPage />);
    expect(screen.getByText(/Ask your administrator/)).toBeInTheDocument();
  });

  it('shows manual install section when expanded', async () => {
    render(<AgentSetupPage />);
    await userEvent.click(screen.getByText('Manual Install'));
    expect(screen.getByText('1. Download the agent binary')).toBeInTheDocument();
    expect(screen.getByText('3. Run the agent')).toBeInTheDocument();
  });

  it('shows what happens next section', () => {
    render(<AgentSetupPage />);
    expect(screen.getByText('What happens next')).toBeInTheDocument();
  });
});
