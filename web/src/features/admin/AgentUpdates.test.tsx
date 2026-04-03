import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useUpdateStore } from '../../state/update-store';
import { AgentUpdates } from './AgentUpdates';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    POST: vi.fn().mockResolvedValue({ data: {}, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

const fakeToken = {
  id: 't1',
  token: 'abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890',
  label: 'Production',
  created_by: 'u1',
  max_uses: 10,
  use_count: 3,
  expires_at: '2099-01-01T00:00:00Z',
  created_at: '2024-01-01T00:00:00Z',
};

const noopFetch = vi.fn();
const noopCreate = vi.fn();
const noopDelete = vi.fn();

describe('AgentUpdates', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useUpdateStore.setState({
      manifests: [],
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
    render(<AgentUpdates />);
    expect(screen.getByText('Agent Settings')).toBeInTheDocument();
  });

  it('renders enrollment token table', () => {
    render(<AgentUpdates />);
    expect(screen.getByText('Enrollment Tokens')).toBeInTheDocument();
    expect(screen.getByText('Production')).toBeInTheDocument();
    expect(screen.getByText('3/10')).toBeInTheDocument();
  });

  it('shows empty state for tokens', () => {
    useUpdateStore.setState({ enrollmentTokens: [] });
    render(<AgentUpdates />);
    expect(screen.getByText('No enrollment tokens yet.')).toBeInTheDocument();
  });

  it('shows create token form when clicked', async () => {
    render(<AgentUpdates />);
    await userEvent.click(screen.getByText('Create Token'));
    expect(screen.getByText('Max Uses (0 = unlimited)')).toBeInTheDocument();
    expect(screen.getByText('Expires In (hours)')).toBeInTheDocument();
  });

  it('masks token by default and reveals on click', async () => {
    render(<AgentUpdates />);
    const maskedButton = screen.getByText(/abcdef12\.\.\.7890/);
    expect(maskedButton).toBeInTheDocument();

    await userEvent.click(maskedButton);
    expect(screen.getByText(fakeToken.token)).toBeInTheDocument();
  });

  it('shows error message', () => {
    useUpdateStore.setState({ error: 'Something went wrong' });
    render(<AgentUpdates />);
    expect(screen.getByText('Something went wrong')).toBeInTheDocument();
  });

  it('does not render manifest or signing key sections', () => {
    render(<AgentUpdates />);
    expect(screen.queryByText('Published Manifests')).not.toBeInTheDocument();
    expect(screen.queryByText('Signing Key')).not.toBeInTheDocument();
  });
});
