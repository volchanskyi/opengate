import type { components } from '../../types/api';

type Rule = components['schemas']['Rule'];
type Rollout = Rule['rollout'];
type Noise = Rule['noise'];
type NoiseLevel = Noise['level'];
type Stage = Rollout['stage'];

/**
 * Lookups use a Map because indexing an object with a value from the wire reaches the
 * prototype chain.
 */

const COMPARATOR_WORDING = new Map<Rule['comparator'], string>([
  ['gt', 'above'],
  ['gte', 'at or above'],
  ['lt', 'below'],
  ['lte', 'at or below'],
]);

const STAGE_WORDING = new Map<Stage, string>([
  ['off', 'Reaching nobody'],
  ['canary', 'First hosts'],
  ['staged', 'Some hosts'],
  ['full', 'Everywhere'],
]);

const NOISE_TONE = new Map<NoiseLevel, string>([
  ['unknown', 'bg-gray-700 text-gray-300'],
  ['quiet', 'bg-gray-700 text-gray-300'],
  ['usual', 'bg-green-900 text-green-200'],
  ['elevated', 'bg-amber-900 text-amber-200'],
  ['high', 'bg-red-900 text-red-200'],
]);

const ATTENTION = new Map<NoiseLevel, number>([
  ['unknown', 0],
  ['quiet', 0],
  ['usual', 0],
  ['elevated', 2],
  ['high', 3],
]);

// An event rule compares no number, so its summary stands alone.
export function watchWording(rule: Rule): string {
  if (rule.kind === 'event') return rule.summary;
  return `${rule.metric} ${COMPARATOR_WORDING.get(rule.comparator) ?? rule.comparator} ${rule.threshold}`;
}

// A stop outranks every other state so it reads apart from an ordinary switch-off.
export function rolloutWording(rollout: Rollout): string {
  if (rollout.kill) return 'Stopped';
  if (!rollout.enabled) return 'Off';
  if (rollout.stage === 'full') return STAGE_WORDING.get('full') ?? 'Everywhere';
  const stage = STAGE_WORDING.get(rollout.stage) ?? rollout.stage;
  return `${stage} — ${rollout.rollout_percent}% of the fleet`;
}

export function noiseWording(noise: Noise): string {
  if (noise.level === 'unknown') {
    return `${noise.recent} in the last hour — nothing to compare against yet`;
  }
  if (noise.recent === 0) return 'Nothing in the last hour';

  const usual = `its usual ${Math.round(noise.baseline_per_hour)} an hour`;
  if (noise.level === 'high') return `${noise.recent} in the last hour — well above ${usual}`;
  if (noise.level === 'elevated') return `${noise.recent} in the last hour — above ${usual}`;
  return `${noise.recent} in the last hour — about ${usual}`;
}

export function noiseTone(level: NoiseLevel): string {
  return NOISE_TONE.get(level) ?? 'bg-gray-700 text-gray-300';
}

export function holdLabel(seconds: number): string {
  const units: readonly (readonly [number, string])[] = [
    [86400, 'day'],
    [3600, 'hour'],
    [60, 'minute'],
  ];
  for (const [size, name] of units) {
    if (seconds >= size && seconds % size === 0) {
      const count = seconds / size;
      return `${count} ${name}${count === 1 ? '' : 's'}`;
    }
  }
  return `${seconds} seconds`;
}

export function selectorWording(selector: Record<string, string>): string {
  const pairs = Object.entries(selector)
    .map(([key, value]) => `${key}=${value}`)
    .sort((a, b) => a.localeCompare(b));
  if (pairs.length === 0) return 'every host at this level';
  return `hosts labelled ${pairs.join(', ')}`;
}

// The other side of a comparison, which is where a reading has to go for an alert to clear.
const CLEARS_WHEN = new Map<Rule['comparator'], string>([
  ['gt', 'below'],
  ['gte', 'below'],
  ['lt', 'above'],
  ['lte', 'above'],
]);

/** Each tunable setting's plain name, and the sentence saying what a value of it does. */
const TUNING = new Map<string, { readonly name: string; readonly explain: (rule: Rule, value: number) => string }>([
  ['threshold', {
    name: 'Alert level',
    explain: (rule, value) =>
      `An alert is raised when ${rule.metric ?? 'the reading'} reads ${COMPARATOR_WORDING.get(rule.comparator) ?? rule.comparator} ${value}.`,
  }],
  ['clear', {
    name: 'All-clear level',
    explain: (rule, value) =>
      `The alert clears once ${rule.metric ?? 'the reading'} reads ${CLEARS_WHEN.get(rule.comparator) ?? 'back past'} ${value}.`,
  }],
  ['sustain_secs', {
    name: 'Must last for',
    explain: (_rule, value) => `The reading must hold for ${holdLabel(value)} before an alert is raised.`,
  }],
  ['window_secs', {
    name: 'Averaging window',
    explain: (_rule, value) => `Readings are averaged over ${holdLabel(value)} before they are compared.`,
  }],
]);

/** A setting's plain name with its stored name beside it, such as "Alert level (threshold)". */
export function settingName(param: string): string {
  const setting = TUNING.get(param);
  return setting ? `${setting.name} (${param})` : param;
}

/** What a typed value of a setting would do on this rule; null for a setting with no sentence. */
export function tuningExplanation(rule: Rule, param: string, value: number): string | null {
  return TUNING.get(param)?.explain(rule, value) ?? null;
}

/** One heading of the rules list and the rules under it, those needing attention first. */
export interface RuleGroup {
  readonly key: string;
  readonly title: string;
  readonly rules: readonly Rule[];
}

/** Rules about readings are host rules; rules about log events are Linux rules. */
export function groupRules(rules: readonly Rule[]): RuleGroup[] {
  const groups: RuleGroup[] = [
    { key: 'host', title: 'Host rules', rules: attentionFirst(rules.filter((r) => r.kind !== 'event')) },
    { key: 'linux', title: 'Linux rules', rules: attentionFirst(rules.filter((r) => r.kind === 'event')) },
  ];
  return groups.filter((g) => g.rules.length > 0);
}

// A stopped rule ranks highest, then a noisy one, then one with a blind spot.
export function ruleAttention(rule: Rule): number {
  if (rule.rollout.kill) return 10;
  const blindSpot = rule.coverage.unsupported > 0 ? 1 : 0;
  return (ATTENTION.get(rule.noise.level) ?? 0) + blindSpot;
}

// Rules sort by attention descending, then by id.
export function attentionFirst(rules: readonly Rule[]): Rule[] {
  return [...rules].sort((a, b) => {
    const byAttention = ruleAttention(b) - ruleAttention(a);
    return byAttention !== 0 ? byAttention : a.id.localeCompare(b.id);
  });
}
