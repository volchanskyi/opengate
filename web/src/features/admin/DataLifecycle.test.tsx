import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../lib/api';
import { useAuthStore } from '../../state/auth-store';
import { DataLifecycle } from './DataLifecycle';

vi.mock('../../lib/api', () => ({
  api: { GET: vi.fn(), POST: vi.fn() },
}));

const orgId = '11111111-1111-1111-1111-111111111111';

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
  });

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
});
