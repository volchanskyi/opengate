import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../lib/api';
import { useAuthStore } from '../../state/auth-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { DataLifecycle } from './DataLifecycle';

vi.mock('../../lib/api', () => ({
  api: { GET: vi.fn(), POST: vi.fn() },
}));

const orgId = '11111111-1111-1111-1111-111111111111';
const addToast = vi.fn();

function jobAt(state: string, overrides: Record<string, unknown> = {}) {
  return {
    data: {
      id: 'job-1', org_id: orgId, scope: 'org', state,
      vm_deleted: state !== 'requested', object_deleted: false, pg_deleted: false, verified: false,
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', ...overrides,
    },
    error: undefined,
  };
}

describe('DataLifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({ orgId, token: 't', user: null, hydrated: true, error: null });
    useToastStore.setState({ addToast });
  });

  afterEach(() => { vi.useRealTimers(); });

  it('warns that the purge is irreversible', () => {
    render(<DataLifecycle />);
    expect(screen.getByText(/irreversible/i)).toBeInTheDocument();
  });

  it('requires a second confirming click before purging', async () => {
    vi.mocked(api.POST).mockResolvedValue(jobAt('central-logical-complete'));
    render(<DataLifecycle />);

    await userEvent.click(screen.getByRole('button', { name: /purge all tenant telemetry/i }));
    expect(api.POST).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: /confirm/i }));
    await waitFor(() => { expect(api.POST).toHaveBeenCalledTimes(1); });
    expect(api.POST).toHaveBeenCalledWith('/api/v1/orgs/{orgId}/purge', { params: { path: { orgId } } });
    expect(addToast).toHaveBeenCalledWith('Tenant purge started', 'success');
  });

  it('shows purge progress after starting', async () => {
    vi.mocked(api.POST).mockResolvedValue(jobAt('central-logical-complete', { vm_deleted: true }));
    render(<DataLifecycle />);

    await userEvent.click(screen.getByRole('button', { name: /purge all tenant telemetry/i }));
    await userEvent.click(screen.getByRole('button', { name: /confirm/i }));

    await waitFor(() => {
      expect(screen.getByText(/Central stores logically erased/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/VictoriaMetrics series deleted/i)).toBeInTheDocument();
  });

  it.each([
    ['requested', 'Requested'],
    ['central-logical-complete', 'Central stores logically erased'],
    ['central-physical-compaction-pending', 'Awaiting VictoriaMetrics compaction'],
    ['object-delete-pending', 'Deleting cold-tier objects'],
    ['edge-erase-pending', 'Awaiting edge erasure'],
    ['complete', 'Complete'],
  ])('renders the exact %s state label', async (state, label) => {
    vi.mocked(api.POST).mockResolvedValue(jobAt(state));
    render(<DataLifecycle />);
    await userEvent.click(screen.getByRole('button', { name: 'Purge all tenant telemetry' }));
    await userEvent.click(screen.getByRole('button', { name: 'Confirm — erase everything' }));
    expect(await screen.findByText(label)).toBeInTheDocument();
  });

  it('cancels confirmation without starting a purge', async () => {
    render(<DataLifecycle />);
    await userEvent.click(screen.getByRole('button', { name: 'Purge all tenant telemetry' }));
    expect(screen.getByRole('button', { name: 'Confirm — erase everything' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.getByRole('button', { name: 'Purge all tenant telemetry' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();
    expect(api.POST).not.toHaveBeenCalled();
  });

  it('disables purge when the authenticated organization is absent', () => {
    useAuthStore.setState({ orgId: null });
    render(<DataLifecycle />);
    expect(screen.getByRole('button', { name: 'Purge all tenant telemetry' })).toBeDisabled();
  });

  it('shows the busy state and hides Cancel until the request settles', async () => {
    let resolvePost!: (value: ReturnType<typeof jobAt>) => void;
    const pending = new Promise<ReturnType<typeof jobAt>>((resolve) => { resolvePost = resolve; });
    vi.mocked(api.POST).mockReturnValue(pending);
    render(<DataLifecycle />);

    await userEvent.click(screen.getByRole('button', { name: 'Purge all tenant telemetry' }));
    await userEvent.click(screen.getByRole('button', { name: 'Confirm — erase everything' }));
    expect(screen.getByRole('button', { name: 'Starting…' })).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();

    resolvePost(jobAt('requested'));
    expect(await screen.findByText('Requested')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Purge all tenant telemetry' })).toBeDisabled();
  });

  it('reports a failed purge request and leaves no job status', async () => {
    vi.mocked(api.POST).mockResolvedValue({ data: undefined, error: { message: 'failed' } } as never);
    render(<DataLifecycle />);
    await userEvent.click(screen.getByRole('button', { name: 'Purge all tenant telemetry' }));
    await userEvent.click(screen.getByRole('button', { name: 'Confirm — erase everything' }));

    await waitFor(() => {
      expect(addToast).toHaveBeenCalledWith('Failed to start tenant purge', 'error');
    });
    expect(screen.queryByText(/Purge status:/)).toBeNull();
    expect(screen.getByRole('button', { name: 'Purge all tenant telemetry' })).toBeEnabled();
  });

  it('polls an in-flight job, renders completion, and then stops polling', async () => {
    vi.useFakeTimers();
    vi.mocked(api.POST).mockResolvedValue(jobAt('requested'));
    vi.mocked(api.GET).mockResolvedValue(jobAt('complete', {
      vm_deleted: true, object_deleted: true, pg_deleted: true, verified: true,
    }));
    render(<DataLifecycle />);
    fireEvent.click(screen.getByRole('button', { name: 'Purge all tenant telemetry' }));
    fireEvent.click(screen.getByRole('button', { name: 'Confirm — erase everything' }));
    await act(async () => {});
    expect(screen.getByText('Requested')).toBeInTheDocument();

    await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
    expect(api.GET).toHaveBeenCalledWith('/api/v1/purge-jobs/{jobId}', { params: { path: { jobId: 'job-1' } } });
    expect(screen.getByText('Complete')).toBeInTheDocument();
    expect(screen.queryByText('(in progress…)')).toBeNull();

    vi.mocked(api.GET).mockClear();
    await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
    expect(api.GET).not.toHaveBeenCalled();
  });

  it('renders every store flag and the last status exactly', async () => {
    vi.mocked(api.POST).mockResolvedValue(jobAt('central-physical-compaction-pending', {
      vm_deleted: true, object_deleted: false, pg_deleted: true, verified: false,
      last_error: 'vm series awaiting compaction',
    }));
    render(<DataLifecycle />);
    await userEvent.click(screen.getByRole('button', { name: 'Purge all tenant telemetry' }));
    await userEvent.click(screen.getByRole('button', { name: 'Confirm — erase everything' }));

    const vm = (await screen.findByText('VictoriaMetrics series deleted')).closest('li')!;
    expect(within(vm).getByText('✓')).toHaveClass('text-green-400');
    const objects = screen.getByText('Cold-tier objects deleted').closest('li')!;
    expect(within(objects).getByText('○')).toHaveClass('text-gray-500');
    expect(screen.getByText('Postgres rows deleted').closest('li')).toHaveTextContent('✓');
    expect(screen.getByText('Central emptiness verified').closest('li')).toHaveTextContent('○');
    expect(screen.getByText('Last status: vm series awaiting compaction')).toBeInTheDocument();
  });
});
