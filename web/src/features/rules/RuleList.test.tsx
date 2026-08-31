import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { components } from '../../types/api';
import { RuleList } from './RuleList';
import { useCatalogueStore } from './state/catalogue-store';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn() } }));

type Rule = components['schemas']['Rule'];

function rollout(over: Partial<Rule['rollout']> = {}): Rule['rollout'] {
  return {
    enabled: true, rollout_percent: 100, kill: false, stage: 'full',
    canary_percent: 1, staged_percent: 10, canary_hold_secs: 3600, staged_hold_secs: 21600,
    ...over,
  };
}

function rule(over: Partial<Rule> = {}): Rule {
  return {
    id: 'disk-critical', version: 1, summary: 'A disk about to fill',
    metric: 'disk.used_percent', comparator: 'gte', threshold: 90,
    group_by: ['device'], group_window_secs: 300, evidence: ['vitals'],
    coverage_requires: ['disk.used_percent'], tunable: {},
    rollout: rollout(),
    coverage: { active: 300, throttled: 0, unsupported: 0, unknown: 12 },
    noise: { recent: 0, baseline_per_hour: 0, level: 'unknown' },
    ...over,
  };
}

function show(rules: Rule[], fleetSize = 312) {
  useCatalogueStore.setState({
    rules, fleetSize, loaded: true, loading: false, error: null,
    fetchCatalogue: async () => {},
  });
  render(
    <MemoryRouter>
      <RuleList />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('RuleList', () => {
  it('says what each rule watches without offering a way to change it', () => {
    show([rule()]);
    expect(screen.getByText('disk.used_percent at or above 90')).toBeInTheDocument();
    expect(screen.queryAllByRole('textbox')).toHaveLength(0);
    expect(screen.queryAllByRole('spinbutton')).toHaveLength(0);
  });

  it('counts the machines running the rule against the fleet it was counted over', () => {
    show([rule()]);
    const row = screen.getByRole('row', { name: /disk-critical/ });
    expect(within(row).getByText('300')).toBeInTheDocument();
    expect(within(row).getByText('/ 312')).toBeInTheDocument();
  });

  it('calls out machines that cannot run the rule at all — a standing blind spot', () => {
    show([rule({ coverage: { active: 300, throttled: 0, unsupported: 6, unknown: 6 } })]);
    expect(screen.getByText(/6 cannot run it/)).toBeInTheDocument();
  });

  it('shows a stop as a stop rather than as the rule being switched off', () => {
    show([rule({ rollout: rollout({ enabled: false, kill: true }) })]);
    expect(screen.getByText('Stopped')).toBeInTheDocument();
    expect(screen.queryByText('Off')).not.toBeInTheDocument();
  });

  it('floats anything wanting attention to the top of the list', () => {
    show([
      rule({ id: 'a-quiet' }),
      rule({ id: 'm-noisy', noise: { recent: 40, baseline_per_hour: 2, level: 'high' } }),
      rule({ id: 'z-stopped', rollout: rollout({ kill: true }) }),
    ]);

    const links = within(screen.getByRole('table')).getAllByRole('link');
    expect(links.map((l) => l.textContent)).toEqual(['z-stopped', 'm-noisy', 'a-quiet']);
  });

  it('shows the recent count against the rule\'s own usual rate', () => {
    show([rule({ noise: { recent: 40, baseline_per_hour: 2, level: 'high' } })]);
    const badge = screen.getByTitle('40 in the last hour — well above its usual 2 an hour');
    expect(badge).toHaveTextContent('40');
  });

  it('says an empty pack is an empty pack rather than showing nothing', () => {
    show([], 0);
    expect(screen.getByText('This server runs no curated rules.')).toBeInTheDocument();
  });
});

/** Renders the list against an arbitrary store state rather than a loaded one. */
function showState(over: Partial<ReturnType<typeof useCatalogueStore.getState>>) {
  useCatalogueStore.setState({
    rules: [], fleetSize: 0, loaded: false, loading: false, error: null,
    fetchCatalogue: async () => {},
    ...over,
  });
  render(
    <MemoryRouter>
      <RuleList />
    </MemoryRouter>,
  );
}

describe('RuleList states', () => {
  // The first load has nothing to show, so it shows the wait rather than an
  // empty table that reads as "this server runs no rules".
  it('shows the wait instead of an empty table on the first load', () => {
    showState({ loading: true, loaded: false });
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
    expect(screen.queryByText('This server runs no curated rules.')).not.toBeInTheDocument();
  });

  // A refresh over rules already on screen keeps them there — replacing a good
  // table with a spinner on every poll would make the page flicker.
  it('keeps the rules on screen while a refresh is in flight', () => {
    showState({ loading: true, loaded: true, rules: [rule()], fleetSize: 312 });
    expect(screen.getByRole('table')).toBeInTheDocument();
    expect(screen.getByText('disk-critical')).toBeInTheDocument();
  });

  it('reads a fetch failure out as an alert', () => {
    showState({ loaded: true, error: 'the catalogue could not be read' });
    expect(screen.getByRole('alert')).toHaveTextContent('the catalogue could not be read');
  });

  it('says nothing about a failure when there was none', () => {
    showState({ loaded: true, rules: [rule()] });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  // "This server runs no curated rules" is a fact about a pack that came back
  // empty. Saying it before the pack arrives states it about a load in flight.
  it('withholds the empty-pack line until the pack has actually arrived', () => {
    showState({ loaded: false, rules: [] });
    expect(screen.queryByText('This server runs no curated rules.')).not.toBeInTheDocument();
  });

  // The fleet count is the denominator the coverage was measured against. With
  // no fleet counted there is no denominator, and "300 / 0" would be a lie.
  it('omits the fleet denominator when no fleet has been counted', () => {
    showState({ loaded: true, rules: [rule()], fleetSize: 0 });
    const row = screen.getByRole('row', { name: /disk-critical/ });
    expect(within(row).getByText('300')).toBeInTheDocument();
    expect(within(row).queryByText(/\/ 0/)).not.toBeInTheDocument();
  });

  // A rule every machine can run has no blind spot, and a "0 cannot run it"
  // would read as a standing hole where there is none.
  it('says nothing about a blind spot when every machine can run the rule', () => {
    showState({
      loaded: true, fleetSize: 312,
      rules: [rule({ coverage: { active: 312, throttled: 0, unsupported: 0, unknown: 0 } })],
    });
    expect(screen.queryByText(/cannot run it/)).not.toBeInTheDocument();
  });
});

describe('RuleList rollout tone', () => {
  /** The rollout badge for the only rule on screen. */
  function badge(over: Partial<Rule['rollout']>): HTMLElement {
    showState({ loaded: true, fleetSize: 312, rules: [rule({ rollout: rollout(over) })] });
    const row = screen.getByRole('row', { name: /disk-critical/ });
    return within(row).getByText(/Stopped|Off|Everywhere|machines/);
  }

  // A stop is an intervention, so it carries the loudest tone on the row —
  // louder than a rollout that is merely part-way out.
  it('gives a stopped rule the loudest tone', () => {
    expect(badge({ kill: true })).toHaveClass('bg-red-900', 'text-red-200');
  });

  it('marks a rule that is only part-way out', () => {
    expect(badge({ stage: 'canary', rollout_percent: 10 })).toHaveClass('bg-amber-900', 'text-amber-200');
  });

  // A rule that is everywhere is where it is meant to be, so it stays quiet.
  it('leaves a fully rolled-out rule quiet', () => {
    expect(badge({ stage: 'full', rollout_percent: 100 })).toHaveClass('bg-gray-700', 'text-gray-300');
  });

  // A rule somebody switched off is not part-way out; it reaches nobody. The
  // amber "in progress" tone would say the opposite.
  it('does not dress a switched-off rule as one still rolling out', () => {
    expect(badge({ enabled: false, rollout_percent: 10 })).toHaveClass('bg-gray-700', 'text-gray-300');
  });
});
