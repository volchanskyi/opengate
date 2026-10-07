import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { components } from '../../types/api';
import { api } from '../../lib/api';
import { useOrganizationStore } from '../organizations';
import { TuningPanel } from './TuningPanel';
import { useRuleStore } from './state/rule-store';

vi.mock('../../lib/api', () => ({
  api: { GET: vi.fn(), PUT: vi.fn(), POST: vi.fn(), DELETE: vi.fn() },
}));

const GET = vi.mocked(api.GET);

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

function site(id: string, name: string) {
  return { id, name, organization_id: 'org-1', created_at: '', updated_at: '' };
}

beforeEach(() => {
  vi.clearAllMocks();
  GET.mockResolvedValue({
    data: [site('site-2', 'Front Desk'), site('site-1', 'back office')],
    response: { ok: true, status: 200 },
  } as never);
  useOrganizationStore.setState({ selectedOrganizationId: 'org-1', customerWanted: 0 });
});

describe('TuningPanel — a new value', () => {
  it('files the value against the site and the setting that were picked', async () => {
    const saveBinding = vi.fn().mockResolvedValue(true);
    useRuleStore.setState({ saveBinding });
    const user = userEvent.setup();
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit />);

    const sites = screen.getByLabelText('Site');
    await vi.waitFor(() => { expect(within(sites).getAllByRole('option')).toHaveLength(3); });
    expect(within(sites).getAllByRole('option').map((o) => o.textContent))
      .toEqual(['Select a site', 'back office', 'Front Desk']);

    await user.selectOptions(screen.getByLabelText('Setting'), 'window_secs');
    await user.selectOptions(sites, 'site-2');
    await user.type(screen.getByLabelText('Value'), '600');
    await user.click(screen.getByRole('button', { name: 'Apply to site' }));

    expect(saveBinding).toHaveBeenCalledWith('cpu-sustained', {
      level: 'site',
      level_key: 'site-2',
      params: { window_secs: 600 },
    });
    expect(screen.getByText(/allowed 60–3600, ships at 300/)).toBeInTheDocument();
  });

  it('names each setting plainly with the stored name beside it', () => {
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit />);
    const settings = within(screen.getByLabelText('Setting')).getAllByRole('option').map((o) => o.textContent);
    expect(settings).toEqual(['Alert level (threshold)', 'Averaging window (window_secs)']);
  });

  it('explains what the typed value will do as it is typed', async () => {
    const user = userEvent.setup();
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit />);

    await user.type(screen.getByLabelText('Value'), '95');
    expect(screen.getByText('An alert is raised when cpu.util reads at or above 95.')).toBeInTheDocument();
  });

  it('marks the shipped value and the typed one on the allowed range', async () => {
    const user = userEvent.setup();
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit />);

    await user.type(screen.getByLabelText('Value'), '95');
    expect(screen.getByRole('img', { name: 'Allowed 50 to 99; ships at 90; typed 95' })).toBeInTheDocument();
  });

  it('sends nothing until both a site and a value are given', async () => {
    const saveBinding = vi.fn().mockResolvedValue(true);
    useRuleStore.setState({ saveBinding });
    const user = userEvent.setup();
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit />);

    await user.type(screen.getByLabelText('Value'), '95');
    await user.click(screen.getByRole('button', { name: 'Apply to site' }));
    expect(saveBinding).not.toHaveBeenCalled();
  });

  it('waits on a customer under "All customers" before offering any site', () => {
    useOrganizationStore.setState({ selectedOrganizationId: null });
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit />);

    const sites = screen.getByLabelText('Site');
    expect(sites).toBeDisabled();
    expect(within(sites).getAllByRole('option').map((o) => o.textContent)).toEqual(['Pick a customer first']);
    expect(GET).not.toHaveBeenCalled();
  });
});

describe('TuningPanel — values in force', () => {
  it('says the rule runs on its shipped values when nothing is set', () => {
    render(<TuningPanel rule={rule()} bindings={[]} clamps={[]} canEdit={false} />);
    expect(screen.getByText('No values set (Ships with the default values)')).toBeInTheDocument();
  });

  it('names the level a value is set at in the product words', () => {
    render(
      <TuningPanel
        rule={rule()}
        bindings={[
          { id: 'b-1', level: 'site', level_key: 'site-1', selector: {}, precedence: 0, params: { threshold: 95 }, updated_by: 'ivan' },
          { id: 'b-2', level: 'device', level_key: 'dev-1', selector: {}, precedence: 0, params: { threshold: 97 }, updated_by: 'ivan' },
        ]}
        clamps={[]}
        canEdit={false}
      />,
    );
    expect(screen.getByText('One site')).toBeInTheDocument();
    expect(screen.getByText('One host')).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'Which hosts' })).toBeInTheDocument();
  });
});
