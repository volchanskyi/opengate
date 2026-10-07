import { describe, it, expect } from 'vitest';
import type { components } from '../../types/api';
import {
  attentionFirst,
  groupRules,
  holdLabel,
  noiseTone,
  noiseWording,
  rolloutWording,
  ruleAttention,
  selectorWording,
  settingName,
  tuningExplanation,
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
    id: 'disk-critical', version: 1, kind: 'reading', severity: 'critical',
    summary: 'A disk about to fill',
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

  it('says what a rule watching the host\'s own words watches', () => {
    const words = rule({
      id: 'linux-oom-kill', kind: 'event', severity: 'critical',
      summary: 'The kernel killed a process to reclaim memory.',
      metric: undefined, comparator: undefined, threshold: undefined,
      coverage_requires: [],
    });
    expect(watchWording(words)).toBe('The kernel killed a process to reclaim memory.');
  });

  it('says how far the rule has reached', () => {
    expect(rolloutWording(rollout())).toBe('Everywhere');
    expect(rolloutWording(rollout({ kill: true }))).toBe('Stopped');
    expect(rolloutWording(rollout({ enabled: false }))).toBe('Off');
    expect(rolloutWording(rollout({ stage: 'canary', rollout_percent: 1 })))
      .toBe('First hosts — 1% of the fleet');
    expect(rolloutWording(rollout({ stage: 'staged', rollout_percent: 10 })))
      .toBe('Some hosts — 10% of the fleet');
  });

  it('a stop outranks being switched off, because they are different actions', () => {
    expect(rolloutWording(rollout({ enabled: false, kill: true }))).toBe('Stopped');
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
    expect(selectorWording({})).toBe('every host at this level');
    expect(selectorWording({ role: 'file-server' })).toBe('hosts labelled role=file-server');
    expect(selectorWording({ role: 'file-server', env: 'production' }))
      .toBe('hosts labelled env=production, role=file-server');
  });

  it('orders labels the way somebody reading them expects, not by code unit', () => {
    expect(selectorWording({ Zone: 'east', az: 'eu-west' }))
      .toBe('hosts labelled az=eu-west, Zone=east');
    expect(selectorWording({ zone: 'east', Ökonomie: 'finance' }))
      .toBe('hosts labelled Ökonomie=finance, zone=east');
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

describe('the settings a rule can be tuned on, in plain words', () => {
  it('gives each setting a plain name with the stored name beside it', () => {
    expect(settingName('threshold')).toBe('Alert level (threshold)');
    expect(settingName('clear')).toBe('All-clear level (clear)');
    expect(settingName('sustain_secs')).toBe('Must last for (sustain_secs)');
    expect(settingName('window_secs')).toBe('Averaging window (window_secs)');
  });

  it('shows a setting it has no plain name for as stored', () => {
    expect(settingName('hysteresis')).toBe('hysteresis');
  });

  it('explains what the typed value will do, on the rule\'s own reading', () => {
    expect(tuningExplanation(rule(), 'threshold', 95))
      .toBe('An alert is raised when disk.used_percent reads at or above 95.');
    expect(tuningExplanation(rule(), 'clear', 85))
      .toBe('The alert clears once disk.used_percent reads below 85.');
    expect(tuningExplanation(rule(), 'sustain_secs', 600))
      .toBe('The reading must hold for 10 minutes before an alert is raised.');
    expect(tuningExplanation(rule(), 'window_secs', 300))
      .toBe('Readings are averaged over 5 minutes before they are compared.');
  });

  it('turns the all-clear the other way for a rule that alerts on a low reading', () => {
    expect(tuningExplanation(rule({ comparator: 'lte' }), 'clear', 15))
      .toBe('The alert clears once disk.used_percent reads above 15.');
  });

  it('explains nothing it has no sentence for, rather than guessing', () => {
    expect(tuningExplanation(rule(), 'hysteresis', 3)).toBeNull();
  });
});

describe('groupRules', () => {
  it('splits the list into host rules and Linux rules by what each rule states it is', () => {
    const groups = groupRules([
      rule({ id: 'linux-oom-kill', kind: 'event' }),
      rule({ id: 'disk-critical' }),
      rule({ id: 'cpu-saturated', noise: noise({ recent: 40, baseline_per_hour: 2, level: 'high' }) }),
    ]);
    expect(groups.map((g) => [g.title, g.rules.map((r) => r.id)])).toEqual([
      ['Host rules', ['cpu-saturated', 'disk-critical']],
      ['Linux rules', ['linux-oom-kill']],
    ]);
  });

  it('keeps an empty group out of the list', () => {
    expect(groupRules([rule()]).map((g) => g.title)).toEqual(['Host rules']);
  });
});
