import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import type { components } from '../../types/api';
import { useAuthStore } from '../../state/auth-store';
import { RuleDetail } from './RuleDetail';
import { useCatalogueStore } from './state/catalogue-store';
import { useRuleStore } from './state/rule-store';

vi.mock('../../lib/api', () => ({
  api: { GET: vi.fn(), PUT: vi.fn(), POST: vi.fn(), DELETE: vi.fn() },
}));

type RuleDetailData = components['schemas']['RuleDetail'];
type Rule = components['schemas']['Rule'];

function rollout(over: Partial<Rule['rollout']> = {}): Rule['rollout'] {
  return {
    enabled: true, rollout_percent: 100, kill: false, stage: 'full',
    canary_percent: 1, staged_percent: 10, canary_hold_secs: 3600, staged_hold_secs: 21600,
    ...over,
  };
}

function detail(over: Partial<RuleDetailData> = {}): RuleDetailData {
  return {
    rule: {
      id: 'disk-critical', version: 2, summary: 'A disk about to fill',
      metric: 'disk.used_percent', comparator: 'gte', threshold: 90, sustain_secs: 300,
      group_by: ['device'], group_window_secs: 300, evidence: ['vitals'],
      coverage_requires: ['disk.used_percent'],
      tunable: { threshold: { min: 50, max: 99, shipped: 90 } },
      rollout: rollout(),
      coverage: { active: 300, throttled: 2, unsupported: 6, unknown: 4 },
      noise: { recent: 4, baseline_per_hour: 4, level: 'usual' },
    },
    bindings: [],
    clamps: [],
    ...over,
  };
}

/** The store with a page already read, so a case is about what is rendered. */
function show(data: RuleDetailData, isAdmin: boolean, fleetSize = 312) {
  useAuthStore.setState({
    user: { id: 'u1', email: 'x@example.com', display_name: 'X', is_admin: isAdmin },
  } as never);
  useCatalogueStore.setState({
    rules: [], fleetSize, loaded: true, loading: false, error: null,
    fetchCatalogue: async () => {},
  });
  useRuleStore.setState({
    detail: data, resolved: null, isLoading: false, error: null,
    fetchRule: async () => {},
  });
  render(
    <MemoryRouter initialEntries={['/rules/disk-critical']}>
      <Routes>
        <Route path="/rules/:ruleId" element={<RuleDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('RuleDetail — what it does', () => {
  it('renders the rule\'s logic as description, never as a form', () => {
    show(detail(), true);
    expect(screen.getByText('disk.used_percent at or above 90')).toBeInTheDocument();

    // No control anywhere carries the rule's own logic: the only fields on the
    // page are the tuning and the rollout pace.
    const forbidden = ['disk.used_percent', 'gte', '90'];
    for (const field of screen.queryAllByRole('textbox')) {
      expect(forbidden).not.toContain((field as HTMLInputElement).value);
    }
  });

  it('says how long a breach must persist and how firings group', () => {
    show(detail(), true);
    // Both the sustain and the grouping window are five minutes here, and both
    // are stated in words rather than left as a number of seconds.
    expect(screen.getAllByText('5 minutes')).toHaveLength(2);
    expect(screen.getByText('device')).toBeInTheDocument();
  });
});

describe('RuleDetail — coverage', () => {
  it('shows the split and says when it does not add up to the fleet', () => {
    show(detail(), false, 312);
    expect(screen.getByText('Cannot run it at all')).toBeInTheDocument();
    expect(screen.getByText('312 of 312 machines accounted for.')).toBeInTheDocument();

    expect(screen.queryByText(/do not add up to the fleet/)).not.toBeInTheDocument();
  });

  it('a split that does not add up is itself the finding', () => {
    show(detail(), false, 400);
    expect(screen.getByText(/do not add up to the fleet/)).toBeInTheDocument();
  });
});

describe('RuleDetail — who may change what', () => {
  it('gives an ordinary member the whole page to read and nothing to press', () => {
    show(detail(), false);
    expect(screen.getByText('A disk about to fill')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Stop for this customer/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Save pace/ })).not.toBeInTheDocument();
  });

  it('gives an administrator the stop switch, apart from the on-off toggle', async () => {
    const setStopped = vi.fn().mockResolvedValue(true);
    show(detail(), true);
    useRuleStore.setState({ setStopped });

    await userEvent.click(screen.getByRole('button', { name: 'Stop for this customer' }));
    expect(setStopped).toHaveBeenCalledWith('disk-critical', 'organization', true);

    await userEvent.click(screen.getByRole('button', { name: 'Stop for every customer' }));
    expect(setStopped).toHaveBeenCalledWith('disk-critical', 'tenant', true);
  });

  it('offers no way at all to switch the automatic pull-back off', () => {
    show(detail(), true);
    for (const box of screen.getAllByRole('checkbox')) {
      expect(box.getAttribute('aria-label') ?? '').not.toMatch(/revert|rollback|pull/i);
    }
    expect(screen.queryByLabelText(/pull-back/i)).not.toBeInTheDocument();
    expect(screen.getByText(/cannot be\s+switched off/)).toBeInTheDocument();
  });
});

describe('RuleDetail — tuning', () => {
  it('lists tuned values narrowest first, with what each is aimed at', () => {
    show(detail({
      bindings: [
        {
          id: 'b-site', level: 'site', level_key: 'office-1', selector: {},
          precedence: 0, params: { threshold: 95 }, updated_by: 'ivan',
        },
        {
          id: 'b-org', level: 'organization', level_key: 'org-1',
          selector: { role: 'file-server' }, precedence: 10,
          params: { threshold: 93 }, updated_by: 'ivan',
        },
      ],
    }), true);

    const rows = screen.getAllByRole('row').filter((r) => within(r).queryByText(/One office|The whole customer/));
    expect(rows[0]).toHaveTextContent('One office');
    expect(rows[1]).toHaveTextContent('The whole customer');
    expect(rows[1]).toHaveTextContent('machines labelled role=file-server');
  });

  it('says what a new rule version had to move, and keeps saying it until acknowledged', async () => {
    const acknowledgeClamp = vi.fn().mockResolvedValue(true);
    show(detail({
      clamps: [{
        id: 'clamp-1', binding_id: 'b-1', rule_id: 'disk-critical', rule_version: 2,
        param: 'threshold', from_value: 98, to_value: 95,
        clamped_at: '2026-08-17T00:00:00Z',
      }],
    }), true);
    useRuleStore.setState({ acknowledgeClamp });

    const notice = screen.getByRole('alert');
    expect(notice).toHaveTextContent('no longer allows threshold at 98');
    expect(notice).toHaveTextContent('it is running at 95');

    await userEvent.click(within(notice).getByRole('button', { name: 'Understood' }));
    expect(acknowledgeClamp).toHaveBeenCalledWith('disk-critical', 'clamp-1');
  });

  it('shows the range a value may be set within, beside what the rule ships', () => {
    show(detail({
      bindings: [{
        id: 'b-org', level: 'organization', level_key: 'org-1', selector: {},
        precedence: 0, params: { threshold: 95 }, updated_by: 'ivan',
      }],
    }), true);
    expect(screen.getAllByText(/allowed 50–99, ships at 90/).length).toBeGreaterThan(0);
  });
});
