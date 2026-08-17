import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../lib/api';
import { RuleCoveragePanel } from './RuleCoveragePanel';
import { useCatalogueStore } from './state/catalogue-store';
import type { components } from '../../types/api';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn() } }));

const mockedGet = vi.mocked(api.GET);

type Rule = components['schemas']['Rule'];

function rule(over: Partial<Rule> = {}): Rule {
  return {
    id: 'cpu.sustained', version: 3, summary: 'CPU pinned for two minutes',
    metric: 'cpu.busy_pct', comparator: 'gt', threshold: 90, group_by: ['device_id'],
    group_window_secs: 900, evidence: ['series'], coverage_requires: ['cpu.busy_pct'],
    tunable: {}, rollout: { enabled: true, rollout_percent: 100, kill: false },
    coverage: { active: 300, throttled: 5, unsupported: 6, unknown: 1 }, ...over,
  };
}

const catalogue = (rules: Rule[], fleetSize: number) => ({
  data: { fleet_size: fleetSize, rules },
  error: undefined,
  response: { ok: true, status: 200 },
});

async function openPanel() {
  const user = userEvent.setup();
  render(<RuleCoveragePanel />);
  await user.click(screen.getByRole('button', { name: /Rule coverage/i }));
  return user;
}

beforeEach(() => {
  vi.clearAllMocks();
  useCatalogueStore.setState({ rules: [], fleetSize: 0, loaded: false, loading: false, error: null });
  mockedGet.mockResolvedValue(catalogue([rule()], 312) as never);
});

describe('RuleCoveragePanel — opening it', () => {
  it('costs nothing until somebody asks for it', () => {
    render(<RuleCoveragePanel />);
    expect(mockedGet).not.toHaveBeenCalled();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('reads the catalogue on the first open', async () => {
    await openPanel();
    expect(await screen.findByRole('table')).toBeInTheDocument();
    expect(mockedGet).toHaveBeenCalledWith('/api/v1/rules', expect.anything());
  });

  it('does not read it again when closed and reopened', async () => {
    const user = await openPanel();
    await screen.findByRole('table');

    await user.click(screen.getByRole('button', { name: /Rule coverage/i }));
    await user.click(screen.getByRole('button', { name: /Rule coverage/i }));
    expect(mockedGet).toHaveBeenCalledTimes(1);
  });
});

describe('RuleCoveragePanel — what a rule is actually watching', () => {
  it('shows all four states, so a standing blind spot cannot hide behind a healthy-looking three', async () => {
    await openPanel();
    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });

    expect(within(row).getByLabelText('Watching')).toHaveTextContent('300');
    expect(within(row).getByLabelText('Throttled')).toHaveTextContent('5');
    expect(within(row).getByLabelText('Cannot evaluate')).toHaveTextContent('6');
    expect(within(row).getByLabelText('Never reported')).toHaveTextContent('1');
  });

  it('says how big the estate the counts were taken against is', async () => {
    await openPanel();
    expect(await screen.findByText(/312 machines/)).toBeInTheDocument();
  });

  it('says what a rule is for, in an operator’s words', async () => {
    await openPanel();
    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).getByText('CPU pinned for two minutes')).toBeInTheDocument();
  });

  it('flags a split that does not add up to the fleet rather than showing it as fact', async () => {
    mockedGet.mockResolvedValue(catalogue([rule({ coverage: { active: 10, throttled: 0, unsupported: 0, unknown: 0 } })], 312) as never);
    await openPanel();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(/10 of 312/);
  });

  it('says nothing about the sum when it agrees with the fleet', async () => {
    await openPanel();
    await screen.findByRole('table');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('marks a rule the stop switch has turned off, so it is not read as watching', async () => {
    mockedGet.mockResolvedValue(catalogue([rule({ rollout: { enabled: true, rollout_percent: 100, kill: true } })], 312) as never);
    await openPanel();

    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).getByText('Stopped')).toBeInTheDocument();
  });

  it('marks a rule that has not been rolled out to the whole estate', async () => {
    mockedGet.mockResolvedValue(catalogue([rule({ rollout: { enabled: true, rollout_percent: 25, kill: false } })], 312) as never);
    await openPanel();

    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).getByText('25% rolled out')).toBeInTheDocument();
  });
});

describe('RuleCoveragePanel — when it cannot be read', () => {
  it('renders an empty catalogue as an answer', async () => {
    mockedGet.mockResolvedValue(catalogue([], 0) as never);
    await openPanel();
    expect(await screen.findByText(/No curated rules/i)).toBeInTheDocument();
  });

  it('surfaces the server’s message', async () => {
    mockedGet.mockResolvedValue({ data: undefined, error: { error: 'catalogue unavailable' }, response: { ok: false, status: 500 } } as never);
    await openPanel();
    expect(await screen.findByRole('alert')).toHaveTextContent('catalogue unavailable');
  });
});
