import { useState } from 'react';
import { HostSelect } from '../../components/HostSelect';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useHostOptions } from '../devices';
import { useCustomerGate } from '../organizations';
import { useRuleStore } from './state/rule-store';

// Resolves the rule for one host as the delivery path does and names what decided each value.
export function ResolvedFor({ ruleId }: { readonly ruleId: string }) {
  const resolved = useRuleStore((s) => s.resolved);
  const resolveFor = useRuleStore((s) => s.resolveFor);
  const clearResolved = useRuleStore((s) => s.clearResolved);
  const customer = useCustomerGate();
  const hosts = useHostOptions(customer);
  const [deviceId, setDeviceId] = useState('');

  const pick = (id: string) => {
    setDeviceId(id);
    fireAndForget(resolveFor(ruleId, id));
  };

  const clear = () => {
    setDeviceId('');
    clearResolved();
  };

  return (
    <section className="bg-gray-800 border border-gray-700 rounded-lg p-4">
      <h2 className="text-sm font-semibold text-gray-200 mb-1">What one host is running</h2>
      <p className="text-xs text-gray-500 mb-3">
        Pick a host to see the values in force on it, and what decided each one.
      </p>

      <div className="flex items-end gap-2">
        <div className="flex flex-col gap-1">
          <span aria-hidden="true" className="text-xs uppercase text-gray-500 font-semibold">Host</span>
          <HostSelect
            label="Host"
            hosts={hosts}
            value={deviceId}
            onChange={pick}
            emptyLabel="Select a host"
            allowEmpty={false}
            disabled={customer === null}
            disabledLabel="Pick a customer first"
          />
        </div>
        {resolved && (
          <button
            type="button"
            onClick={clear}
            className="px-3 py-1 rounded bg-gray-700 hover:bg-gray-600 text-sm"
          >
            Clear
          </button>
        )}
      </div>

      {resolved ? (
        <div className="mt-4">
          <p className="text-sm text-gray-300 mb-2">
            {resolved.delivered
              ? 'This host is running the rule.'
              : 'This host is not getting the rule at all.'}
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
      ) : (
        <p className="mt-4 text-sm text-gray-400">Select a host to see current values.</p>
      )}
    </section>
  );
}
