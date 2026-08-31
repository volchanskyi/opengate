import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { components } from '../../types/api';
import { WhatItDoes } from './WhatItDoes';

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
    id: 'disk-critical', version: 4, summary: 'A disk about to fill',
    metric: 'disk.used_percent', comparator: 'gte', threshold: 90,
    sustain_secs: 300,
    group_by: ['device'], group_window_secs: 300, evidence: ['vitals'],
    coverage_requires: ['disk.used_percent'], tunable: {},
    rollout: rollout(),
    coverage: { active: 300, throttled: 0, unsupported: 0, unknown: 12 },
    noise: { recent: 0, baseline_per_hour: 0, level: 'unknown' },
    ...over,
  };
}

/** The value rendered against a term, which is the <dd> next to that <dt>. */
function valueFor(term: string): string {
  return screen.getByText(term).nextElementSibling?.textContent ?? '';
}

describe('WhatItDoes', () => {
  it('states every fact about the rule under its own term', () => {
    render(<WhatItDoes rule={rule()} />);

    expect(valueFor('Watches')).toBe('disk.used_percent at or above 90');
    expect(valueFor('Must persist for')).toBe('5 minutes');
    expect(valueFor('Alerts grouped by')).toBe('device');
    expect(valueFor('Firings stay one incident for')).toBe('5 minutes');
    expect(valueFor('Carries with an alert')).toBe('vitals');
    expect(valueFor('A machine must be able to read')).toBe('disk.used_percent');
  });

  it('names the version of the pack the rule came from', () => {
    render(<WhatItDoes rule={rule({ version: 7 })} />);
    expect(screen.getByText(/Version 7\./)).toBeInTheDocument();
  });

  // A rule with no hold fires on the reading itself. Saying "0 seconds" would
  // read as a duration somebody chose rather than as the absence of one.
  it('says a rule with no hold fires on the reading', () => {
    render(<WhatItDoes rule={rule({ sustain_secs: 0 })} />);
    expect(valueFor('Must persist for')).toBe('no time at all — it fires on the reading');
  });

  it('renders a hold the operator set as a duration', () => {
    render(<WhatItDoes rule={rule({ sustain_secs: 7200 })} />);
    expect(valueFor('Must persist for')).toBe('2 hours');
  });

  it('joins every entry of a list rather than naming only the first', () => {
    render(<WhatItDoes rule={rule({
      group_by: ['device', 'site'],
      evidence: ['vitals', 'top_processes'],
      coverage_requires: ['disk.used_percent', 'mem.used_percent'],
    })} />);

    expect(valueFor('Alerts grouped by')).toBe('device, site');
    expect(valueFor('Carries with an alert')).toBe('vitals, top_processes');
    expect(valueFor('A machine must be able to read')).toBe('disk.used_percent, mem.used_percent');
  });

  // An empty list is a fact about the rule, so it is worded rather than left
  // blank — a blank cell reads as a value that failed to load.
  it('words an empty list instead of leaving the value blank', () => {
    render(<WhatItDoes rule={rule({ group_by: [], evidence: [], coverage_requires: [] })} />);

    expect(valueFor('Alerts grouped by')).toBe('nothing');
    expect(valueFor('Carries with an alert')).toBe('nothing');
    expect(valueFor('A machine must be able to read')).toBe('nothing in particular');
  });

  // The rule's logic is compiled into the server, so this is description and
  // never a form — a control here would invite "who can unlock it?".
  it('offers no control that would suggest the logic is editable', () => {
    render(<WhatItDoes rule={rule()} />);
    expect(screen.queryAllByRole('textbox')).toHaveLength(0);
    expect(screen.queryAllByRole('spinbutton')).toHaveLength(0);
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });
});
