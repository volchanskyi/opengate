import type { components } from '../../types/api';

type RuleCoverage = components['schemas']['RuleCoverage'];

/** One of the four states every machine in the estate falls into for a rule. */
export type CoverageState = keyof RuleCoverage;

const STATE_LABELS = {
  active: 'Watching',
  throttled: 'Throttled',
  unsupported: 'Cannot evaluate',
  unknown: 'Never reported',
} satisfies Record<CoverageState, string>;

const STATE_LABEL = new Map<CoverageState, string>(
  Object.entries(STATE_LABELS) as [CoverageState, string][],
);

/**
 * All four states, in the order a reader should scan them: what a rule watches
 * first, then the three ways it does not. Showing three of them would make a
 * rule with a standing blind spot look like it was watching a smaller estate.
 */
export const COVERAGE_STATES = Object.keys(STATE_LABELS) as readonly CoverageState[];

export function coverageStateLabel(state: CoverageState): string {
  return STATE_LABEL.get(state) ?? state;
}

/** How many machines the four states account for. */
export function coverageTotal(coverage: RuleCoverage): number {
  return coverage.active + coverage.throttled + coverage.unsupported + coverage.unknown;
}

/** Read one state's count without indexing the coverage object by a variable. */
export function coverageCount(coverage: RuleCoverage, state: CoverageState): number {
  switch (state) {
    case 'active': return coverage.active;
    case 'throttled': return coverage.throttled;
    case 'unsupported': return coverage.unsupported;
    case 'unknown': return coverage.unknown;
  }
}
