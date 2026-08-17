import { useState } from 'react';
import type { components } from '../../types/api';
import { SEVERITIES, STATUSES, severityLabel, statusLabel } from './incident-lifecycle';
import { DEFAULT_QUEUE_FILTERS, type QueueFilters } from './state/queue-store';

type Status = components['schemas']['IncidentStatus'];
type Severity = components['schemas']['IncidentSeverity'];

interface Props {
  readonly filters: QueueFilters;
  readonly onChange: (patch: Partial<QueueFilters>) => void;
}

/** Add or remove one value, keeping the vocabulary's own order. */
function toggle<T>(all: readonly T[], selected: readonly T[], value: T): T[] {
  const next = selected.includes(value)
    ? selected.filter((v) => v !== value)
    : [...selected, value];
  return all.filter((v) => next.includes(v));
}

function Chip({ label, pressed, onClick }: {
  readonly label: string;
  readonly pressed: boolean;
  readonly onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={`px-2 py-1 rounded text-xs ${pressed ? 'bg-blue-600 text-white' : 'bg-gray-700 text-gray-300 hover:bg-gray-600'}`}
    >
      {label}
    </button>
  );
}

/**
 * How the queue is narrowed. Status and severity apply on the click, because
 * each is one decision; the rule and device boxes apply together on submit, so
 * typing a rule name does not spend a request per keystroke.
 */
export function InvestigationFilters({ filters, onChange }: Props) {
  // Seeded from the narrowing already in force, so returning to the queue shows
  // the filters it is actually reading under rather than an empty form. Clearing
  // empties these boxes itself, so there is nothing to mirror back afterwards.
  const [ruleId, setRuleId] = useState(filters.ruleId);
  const [deviceId, setDeviceId] = useState(filters.deviceId);

  const toggleStatus = (status: Status) => {
    onChange({ status: toggle(STATUSES, filters.status, status) });
  };
  const toggleSeverity = (severity: Severity) => {
    onChange({ severity: toggle(SEVERITIES, filters.severity, severity) });
  };

  return (
    <form
      className="flex flex-wrap items-end gap-4 bg-gray-800 border border-gray-700 rounded-lg p-3"
      onSubmit={(e) => {
        e.preventDefault();
        onChange({ ruleId: ruleId.trim(), deviceId: deviceId.trim() });
      }}
    >
      <fieldset className="flex flex-col gap-1">
        <legend className="text-xs text-gray-400 mb-1">Status</legend>
        <div className="flex gap-1">
          {STATUSES.map((s) => (
            <Chip key={s} label={statusLabel(s)} pressed={filters.status.includes(s)} onClick={() => { toggleStatus(s); }} />
          ))}
        </div>
      </fieldset>

      <fieldset className="flex flex-col gap-1">
        <legend className="text-xs text-gray-400 mb-1">Severity</legend>
        <div className="flex gap-1">
          {SEVERITIES.map((s) => (
            <Chip key={s} label={severityLabel(s)} pressed={filters.severity.includes(s)} onClick={() => { toggleSeverity(s); }} />
          ))}
        </div>
      </fieldset>

      <label className="flex flex-col gap-1 text-xs text-gray-400">
        <span>Rule</span>
        <input
          value={ruleId}
          onChange={(e) => setRuleId(e.target.value)}
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 w-44"
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-gray-400">
        <span>Device</span>
        <input
          value={deviceId}
          onChange={(e) => setDeviceId(e.target.value)}
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 w-44"
        />
      </label>

      <div className="flex gap-2">
        <button type="submit" className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 rounded text-xs font-medium">
          Apply
        </button>
        <button
          type="button"
          onClick={() => {
            // Clear the boxes here rather than waiting for the reset to come
            // back round through the store — a button that says Clear clears.
            setRuleId(DEFAULT_QUEUE_FILTERS.ruleId);
            setDeviceId(DEFAULT_QUEUE_FILTERS.deviceId);
            onChange(DEFAULT_QUEUE_FILTERS);
          }}
          className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded text-xs font-medium"
        >
          Clear
        </button>
      </div>
    </form>
  );
}
