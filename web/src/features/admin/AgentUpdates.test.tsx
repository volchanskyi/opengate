import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useUpdateStore } from '../../state/update-store';
import { useToastStore } from '../../state/toast-store';
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

  it('submits create token form', async () => {
    const createFn = vi.fn().mockResolvedValue(undefined);
    useUpdateStore.setState({ createEnrollmentToken: createFn });
    render(<AgentUpdates />);

    await userEvent.click(screen.getByText('Create Token'));

    const labelInput = screen.getByPlaceholderText('e.g. Production rollout');
    await userEvent.clear(labelInput);
    await userEvent.type(labelInput, 'My token');

    await userEvent.click(screen.getByText('Create'));

    expect(createFn).toHaveBeenCalledWith({
      label: 'My token',
      max_uses: 0,
      expires_in_hours: 24,
    });
  });

  it('deletes enrollment token', async () => {
    const deleteFn = vi.fn().mockResolvedValue(undefined);
    useUpdateStore.setState({ deleteEnrollmentToken: deleteFn });
    render(<AgentUpdates />);

    await userEvent.click(screen.getByText('Delete'));

    expect(deleteFn).toHaveBeenCalledWith('t1');
  });

  it('toggles token reveal back to masked', async () => {
    render(<AgentUpdates />);

    const maskedButton = screen.getByText(/abcdef12\.\.\.7890/);
    await userEvent.click(maskedButton);
    expect(screen.getByText(fakeToken.token)).toBeInTheDocument();

    // Click again to re-mask
    await userEvent.click(screen.getByText(fakeToken.token));
    expect(screen.getByText(/abcdef12\.\.\.7890/)).toBeInTheDocument();
  });

  it('does not show cleanup button when all tokens are active', () => {
    render(<AgentUpdates />);
    expect(screen.queryByText(/Cleanup Tokens/)).not.toBeInTheDocument();
  });

  it('shows cleanup button with count when inactive tokens exist', () => {
    const expiredToken = { ...fakeToken, id: 'e1', expires_at: '2020-01-01T00:00:00Z' };
    useUpdateStore.setState({ enrollmentTokens: [fakeToken, expiredToken] });
    render(<AgentUpdates />);
    expect(screen.getByText('Cleanup Tokens (1)')).toBeInTheDocument();
  });

  it('cleanup button requires confirmation', async () => {
    const expiredToken = { ...fakeToken, id: 'e1', expires_at: '2020-01-01T00:00:00Z' };
    const cleanupFn = vi.fn().mockResolvedValue(1);
    useUpdateStore.setState({
      enrollmentTokens: [fakeToken, expiredToken],
      cleanupInactiveTokens: cleanupFn,
    });
    render(<AgentUpdates />);

    const btn = screen.getByText('Cleanup Tokens (1)');
    await userEvent.click(btn);
    expect(screen.getByText('Confirm (1)')).toBeInTheDocument();
    expect(cleanupFn).not.toHaveBeenCalled();

    await userEvent.click(screen.getByText('Confirm (1)'));
    expect(cleanupFn).toHaveBeenCalledTimes(1);
  });

  it('shows token status badges correctly', () => {
    const expiredToken = { ...fakeToken, id: 'e1', expires_at: '2020-01-01T00:00:00Z' };
    const exhaustedToken = { ...fakeToken, id: 'e2', expires_at: '2099-01-01T00:00:00Z', use_count: 10, max_uses: 10 };
    useUpdateStore.setState({ enrollmentTokens: [fakeToken, expiredToken, exhaustedToken] });
    render(<AgentUpdates />);
    expect(screen.getByText('Active')).toBeInTheDocument();
    expect(screen.getByText('Expired')).toBeInTheDocument();
    expect(screen.getByText('Exhausted')).toBeInTheDocument();
  });

  it('cleanup success toast uses singular phrasing when count === 1', async () => {
    const addToast = vi.fn();
    useToastStore.setState({ addToast });
    const expired = { ...fakeToken, id: 'e1', expires_at: '2020-01-01T00:00:00Z' };
    useUpdateStore.setState({
      enrollmentTokens: [fakeToken, expired],
      cleanupInactiveTokens: vi.fn().mockResolvedValue(1),
    });
    render(<AgentUpdates />);
    await userEvent.click(screen.getByText('Cleanup Tokens (1)'));
    await userEvent.click(screen.getByText('Confirm (1)'));
    // count === 1 → no plural suffix
    expect(addToast).toHaveBeenCalledWith('Removed 1 inactive token', 'success');
  });

  it('cleanup success toast pluralizes when count !== 1', async () => {
    const addToast = vi.fn();
    useToastStore.setState({ addToast });
    const expired1 = { ...fakeToken, id: 'e1', expires_at: '2020-01-01T00:00:00Z' };
    const expired2 = { ...fakeToken, id: 'e2', expires_at: '2020-01-01T00:00:00Z' };
    useUpdateStore.setState({
      enrollmentTokens: [fakeToken, expired1, expired2],
      cleanupInactiveTokens: vi.fn().mockResolvedValue(2),
    });
    render(<AgentUpdates />);
    await userEvent.click(screen.getByText('Cleanup Tokens (2)'));
    await userEvent.click(screen.getByText('Confirm (2)'));
    expect(addToast).toHaveBeenCalledWith('Removed 2 inactive tokens', 'success');
  });

  it('cleanup button shows "Cleaning..." label and stays disabled while in-flight', async () => {
    const expired = { ...fakeToken, id: 'e1', expires_at: '2020-01-01T00:00:00Z' };
    let resolve: (v: number) => void = () => undefined;
    useUpdateStore.setState({
      enrollmentTokens: [fakeToken, expired],
      cleanupInactiveTokens: vi.fn().mockReturnValue(new Promise<number>((r) => { resolve = r; })),
    });
    render(<AgentUpdates />);
    await userEvent.click(screen.getByText('Cleanup Tokens (1)'));
    await userEvent.click(screen.getByText('Confirm (1)'));
    const btn = await screen.findByText('Cleaning...');
    expect((btn.closest('button') as HTMLButtonElement).disabled).toBe(true);
    resolve(1);
  });

  it('form resets and closes after submit', async () => {
    const createFn = vi.fn().mockResolvedValue(undefined);
    useUpdateStore.setState({ createEnrollmentToken: createFn });
    render(<AgentUpdates />);
    await userEvent.click(screen.getByText('Create Token'));
    const labelInput = screen.getByPlaceholderText('e.g. Production rollout');
    await userEvent.type(labelInput, 'Pilot');
    await userEvent.click(screen.getByText('Create'));
    // The form collapses (the Label input is unmounted).
    expect(screen.queryByPlaceholderText('e.g. Production rollout')).toBeNull();
  });

  it('Max Uses input falls back to 0 when blanked', async () => {
    const createFn = vi.fn().mockResolvedValue(undefined);
    useUpdateStore.setState({ createEnrollmentToken: createFn });
    render(<AgentUpdates />);
    await userEvent.click(screen.getByText('Create Token'));
    const maxUses = screen.getByLabelText('Max Uses (0 = unlimited)') as HTMLInputElement;
    await userEvent.clear(maxUses);
    await userEvent.click(screen.getByText('Create'));
    expect(createFn).toHaveBeenCalledWith({ label: '', max_uses: 0, expires_in_hours: 24 });
  });

  it('Expires In input falls back to 24 when blanked', async () => {
    const createFn = vi.fn().mockResolvedValue(undefined);
    useUpdateStore.setState({ createEnrollmentToken: createFn });
    render(<AgentUpdates />);
    await userEvent.click(screen.getByText('Create Token'));
    const expires = screen.getByLabelText('Expires In (hours)') as HTMLInputElement;
    await userEvent.clear(expires);
    await userEvent.click(screen.getByText('Create'));
    expect(createFn).toHaveBeenCalledWith({ label: '', max_uses: 0, expires_in_hours: 24 });
  });

  it('Uses cell shows "/max" when max_uses > 0', () => {
    render(<AgentUpdates />);
    // 3/10 row
    expect(screen.getByText('3/10')).toBeInTheDocument();
  });

  it('Uses cell omits "/max" suffix when max_uses === 0 (unlimited)', () => {
    useUpdateStore.setState({
      enrollmentTokens: [{ ...fakeToken, max_uses: 0, use_count: 7 }],
    });
    render(<AgentUpdates />);
    // Should show only "7" — no "/<max>" suffix.
    const cells = Array.from(document.querySelectorAll('td')).map((td) => td.textContent);
    expect(cells).toContain('7');
    expect(cells.every((c) => !/^7\//.test(c ?? ''))).toBe(true);
  });

  it('Label cell falls back to em-dash when label is empty', () => {
    useUpdateStore.setState({
      enrollmentTokens: [{ ...fakeToken, label: '' }],
    });
    render(<AgentUpdates />);
    // The em-dash (—) is the placeholder when label is empty.
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('mounts call fetchEnrollmentTokens', () => {
    const fetchFn = vi.fn();
    useUpdateStore.setState({ fetchEnrollmentTokens: fetchFn });
    render(<AgentUpdates />);
    expect(fetchFn).toHaveBeenCalled();
  });
});
