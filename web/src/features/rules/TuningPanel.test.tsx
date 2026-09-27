import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { components } from '../../types/api';
import { TuningPanel } from './TuningPanel';
import { useRuleStore } from './state/rule-store';

vi.mock('../../lib/api', () => ({
  api: { GET: vi.fn(), PUT: vi.fn(), POST: vi.fn(), DELETE: vi.fn() },
}));

type Rule = components['schemas']['Rule'];

function rule(): Rule {
  return {
    id: 'cpu-sustained', version: 1, kind: 'reading', severity: 'warning', summary: 'A processor held busy',
    metric: 'cpu.util', comparator: 'gte', threshold: 90,
    group_by: ['device'], group_window_secs: 300, evidence: ['vitals'],
    coverage_requires: ['cpu.util'],
    tunable: {
      threshold: { min: 50, max: 99, shipped: 90 },
      window_secs: { min: 60, max: 3600, shipped: 300 },
    },
    rollout: {
      enabled: true, rollout_percent: 100, kill: false, stage: 'full',
      canary_percent: 1, staged_percent: 10, canary_hold_secs: 3600, staged_hold_secs: 21600,
    },
    coverage: { active: 10, throttled: 0, unsupported: 0, unknown: 0 },
    noise: { recent: 0, baseline_per_hour: 0, level: 'unknown' },
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('TuningPanel', () => {
  // A rule adjustable in more than one way files the value against the setting
  // the operator picked, not the first one in the list.
  it('files the value against the setting that was picked', async () => {
    const saveBinding = vi.fn().mockResolvedValue(true);
    useRuleStore.setState({ saveBinding });
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit />);

    await userEvent.selectOptions(screen.getByLabelText('Setting'), 'window_secs');
    await userEvent.type(screen.getByLabelText('Office'), 'office-1');
    await userEvent.type(screen.getByLabelText('Value'), '600');
    await userEvent.click(screen.getByRole('button', { name: 'Set for this office' }));

    expect(saveBinding).toHaveBeenCalledWith('cpu-sustained', {
      level: 'site',
      level_key: 'office-1',
      params: { window_secs: 600 },
    });
    expect(screen.getByText(/allowed 60–3600, ships at 300/)).toBeInTheDocument();
  });
});
