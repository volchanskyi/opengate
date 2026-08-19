import type { components } from '../../types/api';
import { holdLabel, watchWording } from './rule-summary';

type Rule = components['schemas']['Rule'];

const TERM = 'text-xs uppercase text-gray-500 font-semibold';
const VALUE = 'text-sm text-gray-200';

/**
 * What the rule is for, rendered as description and never as a form.
 *
 * A rule's logic is compiled into the server and cost-bounded before it can
 * reach a machine, so there is nothing to edit here. Rendering it as disabled
 * inputs would say the opposite — that this is a form somebody has locked — and
 * invite the question of who can unlock it.
 */
export function WhatItDoes({ rule }: { readonly rule: Rule }) {
  const facts: readonly (readonly [string, string])[] = [
    ['Watches', watchWording(rule)],
    ['Must persist for', rule.sustain_secs ? holdLabel(rule.sustain_secs) : 'no time at all — it fires on the reading'],
    ['Alerts grouped by', rule.group_by.join(', ') || 'nothing'],
    ['Firings stay one incident for', holdLabel(rule.group_window_secs)],
    ['Carries with an alert', rule.evidence.join(', ') || 'nothing'],
    ['A machine must be able to read', rule.coverage_requires.join(', ') || 'nothing in particular'],
  ];

  return (
    <section className="bg-gray-800 border border-gray-700 rounded-lg p-4">
      <h2 className="text-sm font-semibold text-gray-200 mb-1">What it does</h2>
      <p className="text-xs text-gray-500 mb-3">
        Version {rule.version}. What a rule looks for is part of the pack this server runs and is
        not changed here; what can be changed is below.
      </p>
      <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2">
        {facts.map(([term, value]) => (
          <div key={term} className="contents">
            <dt className={TERM}>{term}</dt>
            <dd className={VALUE}>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
