import type { components } from '../../types/api';
import { coverageCount, coverageTotal, type CoverageState } from '../investigations/rule-coverage';

type Coverage = components['schemas']['RuleCoverage'];

const STATES: readonly (readonly [CoverageState, string, string])[] = [
  ['active', 'Running it', 'text-gray-200'],
  ['throttled', 'Stopped running it — it cost more than its allowance', 'text-amber-300'],
  ['unsupported', 'Cannot run it at all', 'text-red-400'],
  ['unknown', 'Not heard from', 'text-gray-400'],
];

// The four states add up to the fleet; a split that does not add up is reported.
export function CoveragePanel({
  coverage,
  fleetSize,
}: {
  readonly coverage: Coverage;
  readonly fleetSize: number;
}) {
  const counted = coverageTotal(coverage);

  return (
    <section className="bg-gray-800 border border-gray-700 rounded-lg p-4">
      <h2 className="text-sm font-semibold text-gray-200 mb-3">Coverage</h2>
      <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2">
        {STATES.map(([state, label, tone]) => (
          <div key={state} className="contents">
            <dt className={`text-sm tabular-nums ${tone}`}>{coverageCount(coverage, state)}</dt>
            <dd className="text-sm text-gray-400">{label}</dd>
          </div>
        ))}
      </dl>
      <p className="mt-3 text-xs text-gray-500">
        {counted} of {fleetSize} machines accounted for.
      </p>
      {counted !== fleetSize && (
        <p role="alert" className="mt-1 text-xs text-red-400">
          These do not add up to the fleet, which means some machines are unaccounted for.
        </p>
      )}
    </section>
  );
}
