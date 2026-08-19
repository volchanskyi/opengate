import { describe, it, expect } from 'vitest';
import type { components } from '../../types/api';
import {
  attentionFirst,
  coveredMachines,
  holdLabel,
  noiseTone,
  noiseWording,
  rolloutWording,
  ruleAttention,
  selectorWording,
  watchWording,
} from './rule-summary';

type Rule = components['schemas']['Rule'];
type Rollout = Rule['rollout'];
type Noise = Rule['noise'];

function rollout(over: Partial<Rollout> = {}): Rollout {
  return {
    enabled: true, rollout_percent: 100, kill: false, stage: 'full',
    canary_percent: 1, staged_percent: 10, canary_hold_secs: 3600, staged_hold_secs: 21600,
    ...over,
  };
}

function noise(over: Partial<Noise> = {}): Noise {
  return { recent: 0, baseline_per_hour: 0, level: 'unknown', ...over };
}

function rule(over: Partial<Rule> = {}): Rule {
  return {
    id: 'disk-critical', version: 1, summary: 'A disk about to fill',
    metric: 'disk.used_percent', comparator: 'gte', threshold: 90,
    group_by: ['device'], group_window_secs: 300, evidence: ['vitals'],
    coverage_requires: ['disk.used_percent'], tunable: {},
    rollout: rollout(), coverage: { active: 10, throttled: 0, unsupported: 0, unknown: 0 },
    noise: noise(),
    ...over,
  };
}

describe('what a rule is doing, in an operator\'s words', () => {
  it('says what the rule watches without implying the logic is editable', () => {
    expect(watchWording(rule())).toBe('disk.used_percent at or above 90');
    expect(watchWording(rule({ comparator: 'lt', threshold: 5 }))).toBe('disk.used_percent below 5');
  });

  it('says how far the rule has reached', () => {
    expect(rolloutWording(rollout())).toBe('Everywhere');
    expect(rolloutWording(rollout({ kill: true }))).toBe('Stopped');
    expect(rolloutWording(rollout({ enabled: false }))).toBe('Off');
    expect(rolloutWording(rollout({ stage: 'canary', rollout_percent: 1 })))
      .toBe('First machines — 1% of the estate');
    expect(rolloutWording(rollout({ stage: 'staged', rollout_percent: 10 })))
      .toBe('Some machines — 10% of the estate');
  });

  it('a stop outranks being switched off, because they are different actions', () => {
    expect(rolloutWording(rollout({ enabled: false, kill: true }))).toBe('Stopped');
  });

  it('counts the machines the rule is actually running on', () => {
    expect(coveredMachines({ active: 300, throttled: 5, unsupported: 6, unknown: 1 })).toBe(300);
  });

  it('reads a waiting period back in the units somebody set it in', () => {
    expect(holdLabel(3600)).toBe('1 hour');
    expect(holdLabel(7200)).toBe('2 hours');
    expect(holdLabel(1800)).toBe('30 minutes');
    expect(holdLabel(60)).toBe('1 minute');
    expect(holdLabel(172800)).toBe('2 days');
  });

  it('says how noisy a rule has been, and against what', () => {
    expect(noiseWording(noise({ level: 'unknown', recent: 4 })))
      .toBe('4 in the last hour — nothing to compare against yet');
    expect(noiseWording(noise({ level: 'quiet', recent: 0, baseline_per_hour: 3 })))
      .toBe('Nothing in the last hour');
    expect(noiseWording(noise({ level: 'usual', recent: 3, baseline_per_hour: 3 })))
      .toBe('3 in the last hour — about its usual 3 an hour');
    expect(noiseWording(noise({ level: 'high', recent: 30, baseline_per_hour: 3 })))
      .toBe('30 in the last hour — well above its usual 3 an hour');
  });

  it('colours the badge against the rule\'s own rate, so a chatty rule is not permanently red', () => {
    expect(noiseTone('unknown')).toContain('gray');
    expect(noiseTone('quiet')).toContain('gray');
    expect(noiseTone('usual')).toContain('green');
    expect(noiseTone('elevated')).toContain('amber');
    expect(noiseTone('high')).toContain('red');
  });

  it('renders the labels a tuned value is aimed at', () => {
    expect(selectorWording({})).toBe('every machine at this level');
    expect(selectorWording({ role: 'file-server' })).toBe('machines labelled role=file-server');
    expect(selectorWording({ role: 'file-server', env: 'production' }))
      .toBe('machines labelled env=production, role=file-server');
  });
});

describe('what floats to the top of the list', () => {
  it('ranks a stopped rule and a noisy one above a quiet one', () => {
    expect(ruleAttention(rule({ rollout: rollout({ kill: true }) })))
      .toBeGreaterThan(ruleAttention(rule({ noise: noise({ level: 'high' }) })));
    expect(ruleAttention(rule({ noise: noise({ level: 'high' }) })))
      .toBeGreaterThan(ruleAttention(rule({ noise: noise({ level: 'elevated' }) })));
    expect(ruleAttention(rule({ noise: noise({ level: 'elevated' }) })))
      .toBeGreaterThan(ruleAttention(rule()));
  });

  it('ranks a rule with a standing blind spot above one watching everything', () => {
    const blind = rule({ id: 'blind', coverage: { active: 4, throttled: 0, unsupported: 6, unknown: 0 } });
    expect(ruleAttention(blind)).toBeGreaterThan(ruleAttention(rule()));
  });

  it('sorts the list so anything needing attention is at the top, ties by name', () => {
    const quiet = rule({ id: 'a-quiet' });
    const alsoQuiet = rule({ id: 'b-quiet' });
    const stopped = rule({ id: 'z-stopped', rollout: rollout({ kill: true }) });
    const noisy = rule({ id: 'm-noisy', noise: noise({ level: 'high' }) });

    expect(attentionFirst([quiet, alsoQuiet, stopped, noisy]).map((r) => r.id))
      .toEqual(['z-stopped', 'm-noisy', 'a-quiet', 'b-quiet']);
  });

  it('does not reorder the caller\'s array', () => {
    const given = [rule({ id: 'a' }), rule({ id: 'z', rollout: rollout({ kill: true }) })];
    attentionFirst(given);
    expect(given.map((r) => r.id)).toEqual(['a', 'z']);
  });
});
