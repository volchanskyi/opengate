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
