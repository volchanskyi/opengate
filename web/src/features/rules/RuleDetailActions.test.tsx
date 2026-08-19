import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { components } from '../../types/api';
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
    id: 'disk-critical', version: 1, summary: 'A disk about to fill',
    metric: 'disk.used_percent', comparator: 'gte', threshold: 90,
    group_by: ['device'], group_window_secs: 300, evidence: ['vitals'],
    coverage_requires: ['disk.used_percent'],
    tunable: { threshold: { min: 50, max: 99, shipped: 90 } },
    rollout: rollout(),
    coverage: { active: 10, throttled: 0, unsupported: 0, unknown: 0 },
    noise: { recent: 0, baseline_per_hour: 0, level: 'unknown' },
  };
}

beforeEach(() => {
  vi.clearAllMocks();
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
    expect(screen.getByText('First machines — 1% of the estate')).toBeInTheDocument();
  });
});

describe('Tuning', () => {
  it('files a new value against the office an operator named', async () => {
    const saveBinding = vi.fn().mockResolvedValue(true);
    useRuleStore.setState({ saveBinding });
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit />);

    await userEvent.type(screen.getByLabelText('Office'), 'office-1');
    await userEvent.type(screen.getByLabelText('Value'), '95');
    await userEvent.click(screen.getByRole('button', { name: 'Set for this office' }));

    expect(saveBinding).toHaveBeenCalledWith('disk-critical', {
      level: 'site',
      level_key: 'office-1',
      params: { threshold: 95 },
    });
  });

  it('sends nothing when the office or the value is missing', async () => {
    const saveBinding = vi.fn().mockResolvedValue(true);
    useRuleStore.setState({ saveBinding });
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit />);

    await userEvent.click(screen.getByRole('button', { name: 'Set for this office' }));
    expect(saveBinding).not.toHaveBeenCalled();
  });

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

  it('says so plainly when nothing is retuned', () => {
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit={false} />);
    expect(screen.getByText(/Nothing is retuned/)).toBeInTheDocument();
  });
});

describe('Resolving for one machine', () => {
  it('asks for the machine an operator named, and shows what decided each number', async () => {
    const resolveFor = vi.fn().mockResolvedValue(undefined);
    useRuleStore.setState({ resolveFor });
    render(<ResolvedFor ruleId="disk-critical" />);

    await userEvent.type(screen.getByLabelText('Machine'), 'fs01');
    await userEvent.click(screen.getByRole('button', { name: 'Show' }));
    expect(resolveFor).toHaveBeenCalledWith('disk-critical', 'fs01');

    useRuleStore.setState({
      resolved: {
        rule_id: 'disk-critical', device_id: 'fs01', delivered: true,
        params: {
          threshold: { value: 95, level: 'site', source: "set on this machine's office" },
        },
      },
    });

    expect(await screen.findByText('95')).toBeInTheDocument();
    expect(screen.getByText("set on this machine's office")).toBeInTheDocument();
    expect(screen.getByText('This machine is running the rule.')).toBeInTheDocument();
  });

  it('says when a machine is not getting the rule at all', () => {
    useRuleStore.setState({
      resolved: {
        rule_id: 'disk-critical', device_id: 'fs01', delivered: false, params: {},
      },
    });
    render(<ResolvedFor ruleId="disk-critical" />);
    expect(screen.getByText('This machine is not getting the rule at all.')).toBeInTheDocument();
  });

  it('asks nothing when no machine is named', async () => {
    const resolveFor = vi.fn().mockResolvedValue(undefined);
    useRuleStore.setState({ resolveFor });
    render(<ResolvedFor ruleId="disk-critical" />);

    await userEvent.click(screen.getByRole('button', { name: 'Show' }));
    expect(resolveFor).not.toHaveBeenCalled();
  });
});
