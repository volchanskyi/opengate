import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../lib/api';
import { RuleCoveragePanel } from './RuleCoveragePanel';
import { useCatalogueStore } from '../rules/state/catalogue-store';
import type { components } from '../../types/api';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn() } }));

const mockedGet = vi.mocked(api.GET);

type Rule = components['schemas']['Rule'];

/** A rule that has reached the whole estate, which is what most cases here are about. */
function fullRollout(): Rule['rollout'] {
  return {
    enabled: true, rollout_percent: 100, kill: false, stage: 'full',
    canary_percent: 1, staged_percent: 10, canary_hold_secs: 3600, staged_hold_secs: 21600,
  };
}

function rule(over: Partial<Rule> = {}): Rule {
  return {
    id: 'cpu.sustained', version: 3, summary: 'CPU pinned for two minutes',
    metric: 'cpu.busy_pct', comparator: 'gt', threshold: 90, group_by: ['device_id'],
    group_window_secs: 900, evidence: ['series'], coverage_requires: ['cpu.busy_pct'],
    tunable: {}, rollout: fullRollout(),
    noise: { recent: 0, baseline_per_hour: 0, level: 'unknown' },
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
    mockedGet.mockResolvedValue(catalogue([rule({ rollout: { ...fullRollout(), kill: true } })], 312) as never);
    await openPanel();

    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).getByText('Stopped')).toBeInTheDocument();
  });

  it('marks a rule that has not been rolled out to the whole estate', async () => {
    mockedGet.mockResolvedValue(catalogue([rule({ rollout: { ...fullRollout(), rollout_percent: 25, stage: 'staged' } })], 312) as never);
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

describe('RuleCoveragePanel — reading the split', () => {
  // A standing blind spot is the one state that is a finding rather than a
  // number, so it is the one state that is coloured. Colouring a throttled
  // count the same way would make a delay look like a hole.
  it('colours a standing blind spot and nothing else', async () => {
    mockedGet.mockResolvedValue(catalogue(
      [rule({ coverage: { active: 300, throttled: 6, unsupported: 6, unknown: 0 } })], 312) as never);
    await openPanel();

    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).getByLabelText('Cannot evaluate')).toHaveClass('text-red-400');
    expect(within(row).getByLabelText('Throttled')).not.toHaveClass('text-red-400');
    expect(within(row).getByLabelText('Watching')).not.toHaveClass('text-red-400');
  });

  it('leaves a rule with no blind spot uncoloured', async () => {
    mockedGet.mockResolvedValue(catalogue(
      [rule({ coverage: { active: 312, throttled: 0, unsupported: 0, unknown: 0 } })], 312) as never);
    await openPanel();

    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).getByLabelText('Cannot evaluate')).not.toHaveClass('text-red-400');
  });

  // The fleet count is the denominator these counts were taken against. With no
  // fleet counted there is none, and "300 / 0" would state a ratio nobody measured.
  it('omits the denominator when no fleet has been counted', async () => {
    mockedGet.mockResolvedValue(catalogue(
      [rule({ coverage: { active: 3, throttled: 0, unsupported: 0, unknown: 0 } })], 0) as never);
    await openPanel();

    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).queryByText(/\/ 0/)).not.toBeInTheDocument();
  });

  // Switched off and stopped are different facts: one is the customer's own
  // choice, the other an intervention. Reading them as one hides which happened.
  it('tells a rule switched off apart from one somebody stopped', async () => {
    mockedGet.mockResolvedValue(catalogue(
      [rule({ rollout: { ...fullRollout(), enabled: false, kill: false } })], 312) as never);
    await openPanel();

    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).getByText('Off')).toBeInTheDocument();
    expect(within(row).queryByText('Stopped')).not.toBeInTheDocument();
  });

  // A rule that is everywhere is the unremarkable case, so it carries no note.
  // A badge on every row would leave nothing for the exceptions to stand out from.
  it('says nothing about the rollout of a rule that has reached everywhere', async () => {
    mockedGet.mockResolvedValue(catalogue([rule()], 312) as never);
    await openPanel();

    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).queryByText('Stopped')).not.toBeInTheDocument();
    expect(within(row).queryByText('Off')).not.toBeInTheDocument();
    expect(within(row).queryByText(/rolled out/)).not.toBeInTheDocument();
  });

  // The first open has nothing to show yet, so it says it is reading rather
  // than rendering an empty table that reads as "no rules are bound".
  it('says it is reading while the first catalogue is in flight', async () => {
    mockedGet.mockReturnValue(new Promise(() => {}) as never);
    await openPanel();

    expect(await screen.findByText('Reading the catalogue…')).toBeInTheDocument();
    expect(screen.queryByText(/No curated rules/i)).not.toBeInTheDocument();
  });

  it('turns the disclosure marker as the panel opens', async () => {
    mockedGet.mockResolvedValue(catalogue([rule()], 312) as never);
    render(<RuleCoveragePanel />);

    const toggle = screen.getByRole('button', { name: /Rule coverage/i });
    expect(toggle.firstElementChild).not.toHaveClass('rotate-90');

    await userEvent.setup().click(toggle);
    expect(toggle.firstElementChild).toHaveClass('rotate-90');
  });
});
