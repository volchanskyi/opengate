import type { components } from '../../types/api';

type RuleCoverage = components['schemas']['RuleCoverage'];

/** One of the four states every host in the fleet falls into for a rule. */
export type CoverageState = keyof RuleCoverage;

const STATE_LABELS = {
  active: 'Watching',
  throttled: 'Paused: too costly',
  unsupported: "Can't run here",
  unknown: 'Not heard from',
} satisfies Record<CoverageState, string>;

const STATE_EXPLANATIONS = {
  active: 'Hosts running this rule now.',
  throttled:
    'Hosts that stopped running this rule because it used more of the host than it is allowed. ' +
    'They are not being checked for it.',
  unsupported:
    "Hosts that can't run this rule at all, for example because they can't take the reading it " +
    'needs. They are never checked for it.',
  unknown:
    "Hosts that haven't reported on this rule, usually because they are offline. Whether they are " +
    'checked is unknown.',
} satisfies Record<CoverageState, string>;

const STATE_EXPLANATION = new Map<CoverageState, string>(
  Object.entries(STATE_EXPLANATIONS) as [CoverageState, string][],
);

const STATE_LABEL = new Map<CoverageState, string>(
  Object.entries(STATE_LABELS) as [CoverageState, string][],
);

/** All four states in scan order: what a rule watches first, then the three ways it does not. */
export const COVERAGE_STATES = Object.keys(STATE_LABELS) as readonly CoverageState[];

export function coverageStateLabel(state: CoverageState): string {
  return STATE_LABEL.get(state) ?? state;
}

/** What a state's hosts are doing about the rule, in the words its heading explains itself with. */
export function coverageStateExplanation(state: CoverageState): string {
  return STATE_EXPLANATION.get(state) ?? '';
}

/** How many hosts the four states account for. */
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
