import { describe, it, expect } from 'vitest';
import type { components } from '../../types/api';
import { COVERAGE_STATES, coverageCount, coverageStateLabel, coverageTotal } from './rule-coverage';

type RuleCoverage = components['schemas']['RuleCoverage'];

const coverage = (over: Partial<RuleCoverage> = {}): RuleCoverage => ({
  active: 0, throttled: 0, unsupported: 0, unknown: 0, ...over,
});

describe('the coverage states', () => {
  it('names all four — three would make a rule look like it watches a smaller estate than it does', () => {
    expect(COVERAGE_STATES).toEqual(['active', 'throttled', 'unsupported', 'unknown']);
  });

  it('says what each state means for the machines counted in it', () => {
    expect(coverageStateLabel('active')).toBe('Watching');
    expect(coverageStateLabel('throttled')).toBe('Throttled');
    expect(coverageStateLabel('unsupported')).toBe('Cannot evaluate');
    expect(coverageStateLabel('unknown')).toBe('Never reported');
  });
});

describe('coverageTotal', () => {
  it('adds all four states, which is what the fleet size is compared against', () => {
    expect(coverageTotal(coverage({ active: 300, throttled: 5, unsupported: 6, unknown: 1 }))).toBe(312);
  });

  it('counts an untouched estate as nothing rather than as a missing number', () => {
    expect(coverageTotal(coverage())).toBe(0);
  });
});

describe('coverageCount', () => {
  const split = coverage({ active: 300, throttled: 5, unsupported: 6, unknown: 1 });

  it('reads each state off the split', () => {
    expect(coverageCount(split, 'active')).toBe(300);
    expect(coverageCount(split, 'throttled')).toBe(5);
    expect(coverageCount(split, 'unsupported')).toBe(6);
    expect(coverageCount(split, 'unknown')).toBe(1);
  });

  it('reads every state the vocabulary declares, and they add to the total', () => {
    const summed = COVERAGE_STATES.reduce((sum, state) => sum + coverageCount(split, state), 0);
    expect(summed).toBe(coverageTotal(split));
  });
});
