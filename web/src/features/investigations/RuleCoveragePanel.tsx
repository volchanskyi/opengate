import { useEffect, useState } from 'react';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { countLabel } from './incident-format';
import { COVERAGE_STATES, coverageCount, coverageStateLabel, coverageTotal } from './rule-coverage';
import { useCatalogueStore } from '../rules/state/catalogue-store';

type Rule = components['schemas']['Rule'];

const HEAD_CELL = 'px-3 py-2 text-left text-xs font-semibold text-gray-400';
const CELL = 'px-3 py-2 text-sm text-gray-300';

/** The one state that is a standing hole in the monitoring rather than a delay. */
const BLIND_SPOT = 'unsupported';

function CoverageCells({ rule, fleetSize }: { readonly rule: Rule; readonly fleetSize: number }) {
  return (
    <>
      {COVERAGE_STATES.map((state) => {
        const count = coverageCount(rule.coverage, state);
        const isHole = state === BLIND_SPOT && count > 0;
        return (
          <td key={state} className={`${CELL} tabular-nums`}>
            <span aria-label={coverageStateLabel(state)} className={isHole ? 'text-red-400 font-semibold' : ''}>
              {count}
            </span>
            {fleetSize > 0 && <span className="text-gray-500 text-xs"> / {fleetSize}</span>}
          </td>
        );
      })}
    </>
  );
}

function RolloutNote({ rule }: { readonly rule: Rule }) {
  if (rule.rollout.kill) {
    return <span className="px-2 py-0.5 rounded text-xs bg-red-900 text-red-200">Stopped</span>;
  }
  if (!rule.rollout.enabled) {
    return <span className="px-2 py-0.5 rounded text-xs bg-gray-700 text-gray-300">Off</span>;
  }
  if (rule.rollout.rollout_percent < 100) {
    return (
      <span className="px-2 py-0.5 rounded text-xs bg-amber-900 text-amber-200">
        {rule.rollout.rollout_percent}% rolled out
      </span>
    );
  }
  return null;
}

/**
 * How much of the estate each curated rule is actually watching.
 *
 * Silent partial coverage is the failure this answers: a rule that cannot be
 * evaluated on six machines looks exactly like one watching all of them until
 * the number is on a screen. All four states are shown together and checked
 * against the fleet, because a split that does not add up is itself the finding.
 */
export function RuleCoveragePanel() {
  const [open, setOpen] = useState(false);
  const rules = useCatalogueStore((s) => s.rules);
  const fleetSize = useCatalogueStore((s) => s.fleetSize);
  const loaded = useCatalogueStore((s) => s.loaded);
  const loading = useCatalogueStore((s) => s.loading);
  const error = useCatalogueStore((s) => s.error);
  const fetchCatalogue = useCatalogueStore((s) => s.fetchCatalogue);

  useEffect(() => {
    if (open && !loaded) fireAndForget(fetchCatalogue());
  }, [open, loaded, fetchCatalogue]);

  const disagreeing = rules.filter((r) => coverageTotal(r.coverage) !== fleetSize);

  return (
    <section className="bg-gray-800 border border-gray-700 rounded-lg p-4">
      <h3>
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="flex items-center gap-2 text-sm font-semibold text-gray-300 hover:text-white"
        >
          <span className={`text-xs transition-transform ${open ? 'rotate-90' : ''}`} aria-hidden="true">&#9654;</span>
          <span>Rule coverage</span>
        </button>
      </h3>

      {open && (
        <div className="mt-3 space-y-2">
          {error && <p role="alert" className="text-sm text-red-400">{error}</p>}
          {loading && !loaded && <p className="text-sm text-gray-400">Reading the catalogue…</p>}

          {loaded && (
            <p className="text-xs text-gray-400">
              Counted against {countLabel(fleetSize, 'machine', 'machines')}.
            </p>
          )}

          {disagreeing.map((r) => (
            <p role="alert" key={r.id} className="text-xs text-red-400">
              {r.id}: the coverage states account for {coverageTotal(r.coverage)} of {fleetSize} machines.
            </p>
          ))}

          {loaded && rules.length === 0 && (
            <p className="text-sm text-gray-500">No curated rules are bound to this customer.</p>
          )}

          {rules.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr>
                    <th className={HEAD_CELL}>Rule</th>
                    {COVERAGE_STATES.map((state) => (
                      <th key={state} className={HEAD_CELL}>{coverageStateLabel(state)}</th>
                    ))}
                    <th className={HEAD_CELL}>Rollout</th>
                  </tr>
                </thead>
                <tbody>
                  {rules.map((r) => (
                    <tr key={r.id} className="border-t border-gray-800">
                      <td className={CELL}>
                        <span className="font-medium text-gray-100">{r.id}</span>
                        <span className="block text-xs text-gray-500">{r.summary}</span>
                      </td>
                      <CoverageCells rule={r} fleetSize={fleetSize} />
                      <td className={CELL}><RolloutNote rule={r} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </section>
  );
}
