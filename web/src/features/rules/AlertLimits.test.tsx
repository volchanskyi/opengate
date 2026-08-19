import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import type { components } from '../../types/api';
import { useAuthStore } from '../../state/auth-store';
import { AlertLimits } from './AlertLimits';
import { useAlertLimitsStore } from './state/alert-limits-store';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn(), PUT: vi.fn() } }));

type Limits = components['schemas']['AlertLimits'];

function limits(over: Partial<Limits> = {}): Limits {
  return {
    organization_hourly: 500, device_hourly: 20,
    max_organization_hourly: 5000, max_device_hourly: 200,
    updated_by: 'ivan', ...over,
  };
}

function show(isAdmin: boolean, over: Partial<Limits> = {}) {
  useAuthStore.setState({
    user: { id: 'u1', email: 'x@example.com', display_name: 'X', is_admin: isAdmin },
  } as never);
  useAlertLimitsStore.setState({
    limits: limits(over), isLoading: false, error: null,
    fetchLimits: async () => {},
  });
  render(
    <MemoryRouter>
      <AlertLimits />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('AlertLimits', () => {
  it('shows both ceilings with how far each may be raised', () => {
    show(false);
    expect(screen.getByLabelText('This customer, per hour')).toHaveValue(500);
    expect(screen.getByLabelText('One machine, per hour')).toHaveValue(20);
    expect(screen.getByText(/At most 5000/)).toBeInTheDocument();
    expect(screen.getByText(/At most 200/)).toBeInTheDocument();
  });

  it('says the per-machine one is enforced on the machine, because a stored row changes nothing', () => {
    show(false);
    expect(screen.getByText(/Enforced on the machine itself/)).toBeInTheDocument();
  });

  it('gives an ordinary member the numbers to read and no way to move them', () => {
    show(false);
    expect(screen.getByLabelText('This customer, per hour')).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
  });

  it('lets an administrator move both halves', async () => {
    const saveLimits = vi.fn().mockResolvedValue(true);
    show(true);
    useAlertLimitsStore.setState({ saveLimits });

    const customer = screen.getByLabelText('This customer, per hour');
    await userEvent.clear(customer);
    await userEvent.type(customer, '2000');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(saveLimits).toHaveBeenCalledWith(2000, 20);
  });

  it('surfaces a refusal rather than swallowing it', () => {
    useAuthStore.setState({
      user: { id: 'u1', email: 'x@example.com', display_name: 'X', is_admin: true },
    } as never);
    useAlertLimitsStore.setState({
      limits: limits(), isLoading: false,
      error: "the customer's hourly budget is 9000, outside 1–5000",
      fetchLimits: async () => {},
    });
    render(
      <MemoryRouter>
        <AlertLimits />
      </MemoryRouter>,
    );

    expect(screen.getByRole('alert')).toHaveTextContent('outside 1–5000');
  });
});
