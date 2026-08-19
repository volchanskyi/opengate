import type { components } from '../../types/api';

type Rule = components['schemas']['Rule'];
type Rollout = Rule['rollout'];
type Noise = Rule['noise'];
type NoiseLevel = Noise['level'];
type Coverage = Rule['coverage'];
type Stage = Rollout['stage'];

/**
 * A rule described in words, never as a form.
 *
 * What a rule watches is compiled into the server and validated before it can
 * reach a machine, so this file renders it as prose. A form control wrapped
 * around a predicate would be the authoring surface the product does not have,
 * and the moment one exists somebody asks why the field is disabled.
 */

/**
 * Every lookup here is a Map rather than an object read by a variable key. A
 * value arriving from the wire is untrusted input, and indexing an object with
 * it reaches the prototype chain; a Map's keys are only the ones put in it.
 */

/** How a comparison reads to a person rather than as an operator symbol. */
const COMPARATOR_WORDING = new Map<Rule['comparator'], string>([
  ['gt', 'above'],
  ['gte', 'at or above'],
  ['lt', 'below'],
  ['lte', 'at or below'],
]);

/** How far the rule has reached, in words. */
const STAGE_WORDING = new Map<Stage, string>([
  ['off', 'Reaching nobody'],
  ['canary', 'First machines'],
  ['staged', 'Some machines'],
  ['full', 'Everywhere'],
]);

/** The colour each badge takes. Relative to the rule's own rate, never absolute. */
const NOISE_TONE = new Map<NoiseLevel, string>([
  ['unknown', 'bg-gray-700 text-gray-300'],
  ['quiet', 'bg-gray-700 text-gray-300'],
  ['usual', 'bg-green-900 text-green-200'],
  ['elevated', 'bg-amber-900 text-amber-200'],
  ['high', 'bg-red-900 text-red-200'],
]);

/** How much a rule's state pulls it up the list. Higher sorts first. */
const ATTENTION = new Map<NoiseLevel, number>([
  ['unknown', 0],
  ['quiet', 0],
  ['usual', 0],
  ['elevated', 2],
  ['high', 3],
]);

/** What the rule watches, and what counts as bad. Description, not a control. */
export function watchWording(rule: Rule): string {
  return `${rule.metric} ${COMPARATOR_WORDING.get(rule.comparator) ?? rule.comparator} ${rule.threshold}`;
}

/**
 * How far a rule has reached. A stop outranks everything, because it is an
 * intervention rather than a customer's ordinary choice and reading the two as
 * one would hide which happened.
 */
export function rolloutWording(rollout: Rollout): string {
  if (rollout.kill) return 'Stopped';
  if (!rollout.enabled) return 'Off';
  if (rollout.stage === 'full') return STAGE_WORDING.get('full') ?? 'Everywhere';
  const stage = STAGE_WORDING.get(rollout.stage) ?? rollout.stage;
  return `${stage} — ${rollout.rollout_percent}% of the estate`;
}

/** How many machines are actually evaluating the rule. */
export function coveredMachines(coverage: Coverage): number {
  return coverage.active;
}

/** How noisy the rule has been, always beside what it is being judged against. */
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

/** The badge's colour. */
export function noiseTone(level: NoiseLevel): string {
  return NOISE_TONE.get(level) ?? 'bg-gray-700 text-gray-300';
}

/** A waiting period read back in the units somebody would have set it in. */
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

/** Which machines a tuned value is aimed at, in words. */
export function selectorWording(selector: Record<string, string>): string {
  const pairs = Object.entries(selector)
    .map(([key, value]) => `${key}=${value}`)
    .sort((a, b) => a.localeCompare(b));
  if (pairs.length === 0) return 'every machine at this level';
  return `machines labelled ${pairs.join(', ')}`;
}

/**
 * How far up the list a rule belongs. A stopped rule is the highest, because
 * somebody reached for the switch and the estate is not being watched for it;
 * then a rule raising far more than it usually does; then one with a standing
 * blind spot, which is monitoring nobody has noticed is missing.
 */
export function ruleAttention(rule: Rule): number {
  if (rule.rollout.kill) return 10;
  const blindSpot = rule.coverage.unsupported > 0 ? 1 : 0;
  return (ATTENTION.get(rule.noise.level) ?? 0) + blindSpot;
}

/**
 * The list order: anything wanting attention at the top, then by name so the
 * rest of the pack sits somewhere a reader can find it again.
 */
export function attentionFirst(rules: readonly Rule[]): Rule[] {
  return [...rules].sort((a, b) => {
    const byAttention = ruleAttention(b) - ruleAttention(a);
    return byAttention !== 0 ? byAttention : a.id.localeCompare(b.id);
  });
}
