import { useEffect, useId } from 'react';
import type { components } from '../../types/api';
import { HostSelect } from '../../components/HostSelect';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useHostOptions } from '../devices';
import { useCustomerGate } from '../organizations';
import { useCatalogueStore } from '../rules';
import { SEVERITIES, STATUSES, severityLabel, statusLabel } from './incident-lifecycle';
import { DEFAULT_QUEUE_FILTERS, type QueueFilters } from './state/queue-store';

type Severity = components['schemas']['IncidentSeverity'];

interface Props {
  readonly filters: QueueFilters;
  readonly onChange: (patch: Partial<QueueFilters>) => void;
}

const LABEL = 'text-xs text-gray-400 mb-1';

/** Add or remove one value, keeping the vocabulary's own order. */
function toggle<T>(all: readonly T[], selected: readonly T[], value: T): T[] {
  const next = selected.includes(value)
    ? selected.filter((v) => v !== value)
    : [...selected, value];
  return all.filter((v) => next.includes(v));
}

function chipTone(on: boolean): string {
  return on ? 'bg-blue-600 text-white' : 'bg-gray-700 text-gray-300 hover:bg-gray-600';
}

function RulePicker({ ruleId, onChange }: { readonly ruleId: string; readonly onChange: (id: string) => void }) {
  const rules = useCatalogueStore((s) => s.rules);
  const loaded = useCatalogueStore((s) => s.loaded);
  const fetchCatalogue = useCatalogueStore((s) => s.fetchCatalogue);

  useEffect(() => {
    if (!loaded) fireAndForget(fetchCatalogue());
  }, [loaded, fetchCatalogue]);

  const ids = rules.map((r) => r.id).sort((a, b) => a.localeCompare(b));

  return (
    <label className="flex flex-col gap-1 text-xs text-gray-400">
      <span>Rule</span>
      <select
        value={ruleId}
        onChange={(e) => { onChange(e.target.value); }}
        className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 w-48"
      >
        <option value="">All rules</option>
        {ids.map((id) => <option key={id} value={id}>{id}</option>)}
      </select>
    </label>
  );
}

function HostPicker({ deviceId, onChange }: { readonly deviceId: string; readonly onChange: (id: string) => void }) {
  const customer = useCustomerGate();
  const hosts = useHostOptions(customer);

  return (
    <div className="flex flex-col gap-1">
      <span aria-hidden="true" className="text-xs text-gray-400">Host</span>
      <HostSelect
        label="Host"
        hosts={hosts}
        value={deviceId}
        onChange={onChange}
        emptyLabel="All hosts"
        disabled={customer === null}
        disabledLabel="Pick a customer first"
      />
    </div>
  );
}

/** Queue filters; every pick applies at once, and Clear goes back to the new incidents. */
export function InvestigationFilters({ filters, onChange }: Props) {
  const statusLabelId = useId();
  const toggleSeverity = (severity: Severity) => {
    onChange({ severity: toggle(SEVERITIES, filters.severity, severity) });
  };

  return (
    <div className="flex flex-wrap items-end gap-4 bg-gray-800 border border-gray-700 rounded-lg p-3">
      <div className="flex flex-col gap-1">
        <span id={statusLabelId} className={LABEL}>Status</span>
        <div role="radiogroup" aria-labelledby={statusLabelId} className="flex gap-1">
          {STATUSES.map((s) => (
            <button
              key={s}
              type="button"
              role="radio"
              aria-checked={filters.status === s}
              onClick={() => { onChange({ status: s }); }}
              className={`px-2 py-1 rounded text-xs ${chipTone(filters.status === s)}`}
            >
              {statusLabel(s)}
            </button>
          ))}
        </div>
      </div>

      <fieldset className="flex flex-col gap-1">
        <legend className={LABEL}>Severity</legend>
        <div className="flex gap-1">
          {SEVERITIES.map((s) => (
            <button
              key={s}
              type="button"
              aria-pressed={filters.severity.includes(s)}
              onClick={() => { toggleSeverity(s); }}
              className={`px-2 py-1 rounded text-xs ${chipTone(filters.severity.includes(s))}`}
            >
              {severityLabel(s)}
            </button>
          ))}
        </div>
      </fieldset>

      <RulePicker ruleId={filters.ruleId} onChange={(ruleId) => { onChange({ ruleId }); }} />
      <HostPicker deviceId={filters.deviceId} onChange={(deviceId) => { onChange({ deviceId }); }} />

      <button
        type="button"
        onClick={() => { onChange(DEFAULT_QUEUE_FILTERS); }}
        className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded text-xs font-medium"
      >
        Clear
      </button>
    </div>
  );
}
