import { useEffect } from 'react';
import { Link } from 'react-router';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { LoadingSpinner } from '../../components/LoadingSpinner';
import { NoiseBadge } from './NoiseBadge';
import { attentionFirst, coveredMachines, rolloutWording, watchWording } from './rule-summary';
import { useCatalogueStore } from './state/catalogue-store';

type Rule = components['schemas']['Rule'];

const HEAD = 'px-3 py-2 text-left text-xs font-semibold text-gray-400';
const CELL = 'px-3 py-2 text-sm text-gray-300';

/** The state that is a standing hole in the monitoring rather than a delay. */
function BlindSpot({ rule }: { readonly rule: Rule }) {
  if (rule.coverage.unsupported === 0) return null;
  return (
    <span className="text-red-400 text-xs">
      {' '}
      · {rule.coverage.unsupported} cannot run it
    </span>
  );
}

/**
 * How loudly a rollout reads: somebody stopped it, it is part-way out, or it is
 * simply where it is meant to be.
 */
function rolloutTone(rollout: Rule['rollout']): string {
  if (rollout.kill) return 'bg-red-900 text-red-200';
  if (rollout.enabled && rollout.rollout_percent < 100) return 'bg-amber-900 text-amber-200';
  return 'bg-gray-700 text-gray-300';
}

function RolloutCell({ rule }: { readonly rule: Rule }) {
  const tone = rolloutTone(rule.rollout);
  return <span className={`px-2 py-0.5 rounded text-xs ${tone}`}>{rolloutWording(rule.rollout)}</span>;
}

/**
 * Every curated rule, and what it is doing to the selected customer's estate.
 *
 * Sorted so anything wanting attention floats up: a rule somebody stopped, one
 * raising far more than it usually does, one with machines that cannot run it at
 * all. A pack listed alphabetically reads as a wall of rows in which nothing is
 * more urgent than anything else, which is how a stopped rule stays stopped.
 */
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

  return (
    <div className="p-6">
      <header className="flex items-center justify-between mb-4">
        <div>
          <h1 className="text-xl font-bold">Rules</h1>
          <p className="text-sm text-gray-400">
            What the fleet is watched for, and how much of it each rule is reaching.
          </p>
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
        <p role="alert" className="mb-4 text-sm text-red-400">
          {error}
        </p>
      )}

      <table className="w-full bg-gray-800 border border-gray-700 rounded-lg overflow-hidden">
        <thead className="bg-gray-750">
          <tr>
            <th className={HEAD}>Rule</th>
            <th className={HEAD}>Watches</th>
            <th className={HEAD}>Reach</th>
            <th className={HEAD}>Machines</th>
            <th className={HEAD}>Recent alerts</th>
          </tr>
        </thead>
        <tbody>
          {attentionFirst(rules).map((rule) => (
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
              <td className={`${CELL} tabular-nums`}>
                {coveredMachines(rule.coverage)}
                {fleetSize > 0 && <span className="text-gray-500 text-xs"> / {fleetSize}</span>}
                <BlindSpot rule={rule} />
              </td>
              <td className={CELL}>
                <NoiseBadge noise={rule.noise} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {loaded && rules.length === 0 && (
        <p className="mt-4 text-sm text-gray-400">This server runs no curated rules.</p>
      )}
    </div>
  );
}
