import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { components } from '../../types/api';
import { api } from '../../lib/api';
import { useOrganizationStore } from '../organizations';
import { ResolvedFor } from './ResolvedFor';
import { RolloutPanel } from './RolloutPanel';
import { TuningPanel } from './TuningPanel';
import { useRuleStore } from './state/rule-store';

vi.mock('../../lib/api', () => ({
  api: { GET: vi.fn(), PUT: vi.fn(), POST: vi.fn(), DELETE: vi.fn() },
}));

type Rule = components['schemas']['Rule'];
type Rollout = Rule['rollout'];

function rollout(over: Partial<Rollout> = {}): Rollout {
  return {
    enabled: true, rollout_percent: 100, kill: false, stage: 'full',
    canary_percent: 1, staged_percent: 10, canary_hold_secs: 3600, staged_hold_secs: 21600,
    ...over,
  };
}

function rule(): Rule {
  return {
    id: 'disk-critical', version: 1, kind: 'reading', severity: 'critical', summary: 'A disk about to fill',
    metric: 'disk.used_percent', comparator: 'gte', threshold: 90,
    group_by: ['device'], group_window_secs: 300, evidence: ['vitals'],
    coverage_requires: ['disk.used_percent'],
    tunable: { threshold: { min: 50, max: 99, shipped: 90 } },
    rollout: rollout(),
    coverage: { active: 10, throttled: 0, unsupported: 0, unknown: 0 },
    noise: { recent: 0, baseline_per_hour: 0, level: 'unknown' },
  };
}

function host(id: string, hostname: string, status: 'online' | 'offline') {
  return {
    id, hostname, status, organization_id: 'org-1', site_id: '', os: 'linux', agent_version: '',
    capabilities: [], last_seen: '', created_at: '', updated_at: '',
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.GET).mockResolvedValue({
    data: [host('fs02', 'fs02', 'offline'), host('fs01', 'fs01', 'online')],
    response: { ok: true, status: 200 },
  } as never);
  useOrganizationStore.setState({ selectedOrganizationId: 'org-1', customerWanted: 0 });
  useRuleStore.setState({ detail: null, resolved: null, isLoading: false, error: null });
});

describe('Rollout pace', () => {
  it('saves the populations and holds an operator typed, and nothing else', async () => {
    const saveRollout = vi.fn().mockResolvedValue(true);
    useRuleStore.setState({ saveRollout });
    render(<RolloutPanel ruleId="disk-critical" rollout={rollout()} canEdit />);

    const canary = screen.getByLabelText('First stage reaches');
    await userEvent.clear(canary);
    await userEvent.type(canary, '5');
    await userEvent.click(screen.getByRole('button', { name: 'Save pace' }));

    expect(saveRollout).toHaveBeenCalledWith('disk-critical', {
      enabled: true,
      canary_percent: 5,
      staged_percent: 10,
      canary_hold_secs: 3600,
      staged_hold_secs: 21600,
    });
  });

  it('lets a stop be lifted, which is a different action from switching the rule on', async () => {
    const setStopped = vi.fn().mockResolvedValue(true);
    useRuleStore.setState({ setStopped });
    render(<RolloutPanel ruleId="disk-critical" rollout={rollout({ kill: true })} canEdit />);

    await userEvent.click(screen.getByRole('button', { name: 'Let it run for this customer' }));
    expect(setStopped).toHaveBeenCalledWith('disk-critical', 'organization', false);
  });

  it('says how far the rule has reached, in words rather than as a percentage alone', () => {
    render(
      <RolloutPanel
        ruleId="disk-critical"
        rollout={rollout({ stage: 'canary', rollout_percent: 1 })}
        canEdit={false}
      />,
    );
    expect(screen.getByText('First hosts — 1% of the fleet')).toBeInTheDocument();
  });
});

describe('Tuning', () => {
  it('removes a tuned value', async () => {
    const removeBinding = vi.fn().mockResolvedValue(true);
    useRuleStore.setState({ removeBinding });
    render(
      <TuningPanel
        rule={rule()}
        bindings={[{
          id: 'b-1', level: 'site', level_key: 'office-1', selector: {},
          precedence: 0, params: { threshold: 95 }, updated_by: 'ivan',
        }]}
        clamps={[]}
        canEdit
      />,
    );

    await userEvent.click(screen.getByRole('button', { name: 'Remove' }));
    expect(removeBinding).toHaveBeenCalledWith('disk-critical', 'b-1');
  });

  it('says so plainly when nothing is set', () => {
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit={false} />);
    expect(screen.getByText('No values set (Ships with the default values)')).toBeInTheDocument();
  });
});

describe('Resolving for one host', () => {
  async function openHosts() {
    await vi.waitFor(() => { expect(api.GET).toHaveBeenCalled(); });
    await act(async () => { await Promise.resolve(); });
    fireEvent.click(screen.getByRole('button', { name: /Host/ }));
    return within(screen.getByRole('listbox', { name: 'Host' }));
  }

  async function pickHost(name: RegExp) {
    fireEvent.click((await openHosts()).getByRole('option', { name }));
  }

  it('shows the values in force as soon as a host is picked, and what decided each', async () => {
    const resolveFor = vi.fn().mockResolvedValue(undefined);
    useRuleStore.setState({ resolveFor });
    render(<ResolvedFor ruleId="disk-critical" />);

    expect(screen.getByText('Select a host to see current values.')).toBeInTheDocument();
    await pickHost(/fs01/);
    expect(resolveFor).toHaveBeenCalledWith('disk-critical', 'fs01');

    useRuleStore.setState({
      resolved: {
        rule_id: 'disk-critical', device_id: 'fs01', delivered: true,
        params: {
          threshold: { value: 95, level: 'site', source: "set on this host's site" },
        },
      },
    });

    expect(await screen.findByText('95')).toBeInTheDocument();
    expect(screen.getByText("set on this host's site")).toBeInTheDocument();
    expect(screen.getByText('This host is running the rule.')).toBeInTheDocument();
  });

  it('offers the hosts by name, each marked online or offline', async () => {
    render(<ResolvedFor ruleId="disk-critical" />);
    expect(screen.getByRole('button', { name: /Host/ })).toHaveTextContent('Select a host');
    const list = await openHosts();
    expect(list.getAllByRole('option').map((o) => o.textContent)).toEqual(['fs01online', 'fs02offline']);
  });

  it('says when a host is not getting the rule at all', () => {
    useRuleStore.setState({
      resolved: {
        rule_id: 'disk-critical', device_id: 'fs01', delivered: false, params: {},
      },
    });
    render(<ResolvedFor ruleId="disk-critical" />);
    expect(screen.getByText('This host is not getting the rule at all.')).toBeInTheDocument();
  });

  it('waits on a customer under "All customers" and asks for one', () => {
    useOrganizationStore.setState({ selectedOrganizationId: null });
    render(<ResolvedFor ruleId="disk-critical" />);
    const control = screen.getByRole('button', { name: /Host/ });
    expect(control).toBeDisabled();
    expect(control).toHaveTextContent('Pick a customer first');
  });
});
