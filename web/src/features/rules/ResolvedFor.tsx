import { useState } from 'react';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useRuleStore } from './state/rule-store';

/**
 * The rule as one named machine is actually running it.
 *
 * "Why is FS01 at 95?" is the question the tuning section exists to answer, and
 * a list of values filed at four levels does not answer it on its own. This
 * resolves the rule the way the delivery path does and names what decided each
 * number, so the answer is read rather than worked out.
 */
export function ResolvedFor({ ruleId }: { readonly ruleId: string }) {
  const resolved = useRuleStore((s) => s.resolved);
  const resolveFor = useRuleStore((s) => s.resolveFor);
  const clearResolved = useRuleStore((s) => s.clearResolved);
  const [deviceId, setDeviceId] = useState('');

  const ask = () => {
    if (!deviceId) return;
    fireAndForget(resolveFor(ruleId, deviceId));
  };

  return (
    <section className="bg-gray-800 border border-gray-700 rounded-lg p-4">
      <h2 className="text-sm font-semibold text-gray-200 mb-1">What one machine is running</h2>
      <p className="text-xs text-gray-500 mb-3">
        Name a machine to see the values in force on it, and what decided each one.
      </p>

      <div className="flex items-end gap-2">
        <label className="flex flex-col gap-1">
          <span className="text-xs uppercase text-gray-500 font-semibold">Machine</span>
          <input
            className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm w-72"
            placeholder="machine id"
            aria-label="Machine"
            value={deviceId}
            onChange={(e) => { setDeviceId(e.target.value); }}
          />
        </label>
        <button
          type="button"
          onClick={ask}
          className="px-3 py-1 rounded bg-blue-600 hover:bg-blue-500 text-sm"
        >
          Show
        </button>
        {resolved && (
          <button
            type="button"
            onClick={clearResolved}
            className="px-3 py-1 rounded bg-gray-700 hover:bg-gray-600 text-sm"
          >
            Clear
          </button>
        )}
      </div>

      {resolved && (
        <div className="mt-4">
          <p className="text-sm text-gray-300 mb-2">
            {resolved.delivered
              ? 'This machine is running the rule.'
              : 'This machine is not getting the rule at all.'}
          </p>
          <dl className="grid grid-cols-[max-content_max-content_1fr] gap-x-6 gap-y-1">
            {Object.entries(resolved.params)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([name, param]) => (
                <div key={name} className="contents">
                  <dt className="text-xs uppercase text-gray-500 font-semibold">{name}</dt>
                  <dd className="text-sm text-gray-200 tabular-nums">{param.value}</dd>
                  <dd className="text-sm text-gray-400">{param.source}</dd>
                </div>
              ))}
          </dl>
        </div>
      )}
    </section>
  );
}
