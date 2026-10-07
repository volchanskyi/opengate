import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { LoadingSpinner } from '../../components/LoadingSpinner';
import { NoiseBadge } from './NoiseBadge';
import { COVERAGE_STATES, coverageCount, coverageStateLabel, coverageTotal } from './rule-coverage';
import { groupRules, rolloutWording, watchWording, type RuleGroup } from './rule-summary';
import { useCatalogueStore } from './state/catalogue-store';

type Rule = components['schemas']['Rule'];

const HEAD = 'px-3 py-2 text-left text-xs font-semibold text-gray-400';
const CELL = 'px-3 py-2 text-sm text-gray-300';

/** The coverage state that marks a standing hole in the monitoring. */
const BLIND_SPOT = 'unsupported';

function rolloutTone(rollout: Rule['rollout']): string {
  if (rollout.kill) return 'bg-red-900 text-red-200';
  if (rollout.enabled && rollout.rollout_percent < 100) return 'bg-amber-900 text-amber-200';
  return 'bg-gray-700 text-gray-300';
}

function RolloutCell({ rule }: { readonly rule: Rule }) {
  const tone = rolloutTone(rule.rollout);
  return <span className={`px-2 py-0.5 rounded text-xs ${tone}`}>{rolloutWording(rule.rollout)}</span>;
}

function CoverageCells({ rule, fleetSize }: { readonly rule: Rule; readonly fleetSize: number }) {
  return (
    <>
      {COVERAGE_STATES.map((state) => {
        const count = coverageCount(rule.coverage, state);
        const isHole = state === BLIND_SPOT && count > 0;
        return (
          <td key={state} aria-label={coverageStateLabel(state)} className={`${CELL} tabular-nums whitespace-nowrap`}>
            <span className={isHole ? 'text-red-400 font-semibold' : ''}>{count}</span>
            {fleetSize > 0 && <span className="text-gray-500 text-xs"> / {fleetSize}</span>}
          </td>
        );
      })}
    </>
  );
}

function RuleGroupTable({ group, fleetSize }: { readonly group: RuleGroup; readonly fleetSize: number }) {
  const [open, setOpen] = useState(true);

  return (
    <section aria-label={group.title} className="space-y-2">
      <h2>
        <button
          type="button"
          aria-expanded={open}
          onClick={() => { setOpen((v) => !v); }}
          className="flex items-center gap-2 text-sm font-semibold text-gray-200 hover:text-white"
        >
          <span aria-hidden="true" className={`text-xs transition-transform ${open ? 'rotate-90' : ''}`}>&#9654;</span>
          <span>{group.title}</span>{' '}
          <span className="text-gray-400 font-normal">({group.rules.length})</span>
        </button>
      </h2>

      {open && (
        <div className="overflow-x-auto">
          <table className="w-full bg-gray-800 border border-gray-700 rounded-lg overflow-hidden">
            <thead className="bg-gray-750">
              <tr>
                <th className={HEAD}>Rule</th>
                <th className={HEAD}>Watches</th>
                <th className={HEAD}>Rolled out</th>
                {COVERAGE_STATES.map((state) => (
                  <th key={state} className={HEAD}>{coverageStateLabel(state)}</th>
                ))}
                <th className={HEAD}>Recent alerts</th>
              </tr>
            </thead>
            <tbody>
              {group.rules.map((rule) => (
                <tr key={rule.id} className="border-t border-gray-700">
                  <td className={CELL}>
                    <Link to={`/rules/${rule.id}`} className="text-blue-400 hover:text-blue-300">
                      {rule.id}
                    </Link>
                    <p className="text-xs text-gray-500">{rule.summary}</p>
                  </td>
                  <td className={CELL}>{watchWording(rule)}</td>
                  <td className={CELL}>
                    <RolloutCell rule={rule} />
                  </td>
                  <CoverageCells rule={rule} fleetSize={fleetSize} />
                  <td className={CELL}>
                    <NoiseBadge noise={rule.noise} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

// Within a group, a stopped, noisy or partly unsupported rule floats to the top.
export function RuleList() {
  const rules = useCatalogueStore((s) => s.rules);
  const fleetSize = useCatalogueStore((s) => s.fleetSize);
  const loaded = useCatalogueStore((s) => s.loaded);
  const loading = useCatalogueStore((s) => s.loading);
  const error = useCatalogueStore((s) => s.error);
  const fetchCatalogue = useCatalogueStore((s) => s.fetchCatalogue);

  useEffect(() => {
    fireAndForget(fetchCatalogue());
  }, [fetchCatalogue]);

  if (loading && !loaded) return <LoadingSpinner />;

  // A fleet of zero means none was counted, so there is nothing to disagree with.
  const disagreeing = fleetSize > 0 ? rules.filter((r) => coverageTotal(r.coverage) !== fleetSize) : [];

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">Rules</h1>
          <p className="text-sm text-gray-400">
            What the fleet is watched for, and how much of it each rule is reaching.
          </p>
          {loaded && fleetSize > 0 && (
            <p className="text-xs text-gray-500 mt-1">Counted against {fleetSize} hosts</p>
          )}
        </div>
        <nav className="flex gap-3 text-sm">
          <Link to="/rules/labels" className="text-blue-400 hover:text-blue-300">
            Labels
          </Link>
          <Link to="/rules/alert-limits" className="text-blue-400 hover:text-blue-300">
            Alert limits
          </Link>
        </nav>
      </header>

      {error && (
        <p role="alert" className="text-sm text-red-400">
          {error}
        </p>
      )}

      {disagreeing.map((r) => (
        <p role="alert" key={r.id} className="text-xs text-red-400">
          {r.id}: the coverage counts account for {coverageTotal(r.coverage)} of {fleetSize} hosts.
        </p>
      ))}

      {groupRules(rules).map((group) => (
        <RuleGroupTable key={group.key} group={group} fleetSize={fleetSize} />
      ))}

      {loaded && rules.length === 0 && (
        <p className="text-sm text-gray-400">This server runs no curated rules.</p>
      )}
    </div>
  );
}
