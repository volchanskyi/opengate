import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { HostLabel } from '../../components/HostSelect';
import { LoadingSpinner } from '../../components/LoadingSpinner';
import { useAuthStore } from '../../state/auth-store';
import { useHostNames, useHostOptions } from '../devices';
import { useCustomerGate, useOrganizationStore } from '../organizations';
import { useDeviceTagsStore } from './state/device-tags-store';

type DeviceTagLabel = components['schemas']['DeviceTagLabel'];

const CELL = 'px-3 py-2 text-sm text-gray-300';
const HEAD = 'px-3 py-2 text-left text-xs font-semibold text-gray-400';

function AddLabel() {
  const createLabel = useDeviceTagsStore((s) => s.createLabel);
  const [key, setKey] = useState('');
  const [value, setValue] = useState('');

  const submit = () => {
    if (!key || !value) return;
    fireAndForget(createLabel(key, value));
    setValue('');
  };

  return (
    <div className="flex items-end gap-2">
      <label className="flex flex-col gap-1">
        <span className="text-xs uppercase text-gray-500 font-semibold">Key</span>
        <input
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm w-40"
          aria-label="Key"
          placeholder="role"
          value={key}
          onChange={(e) => { setKey(e.target.value); }}
        />
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs uppercase text-gray-500 font-semibold">Value</span>
        <input
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm w-40"
          aria-label="Value"
          placeholder="file-server"
          value={value}
          onChange={(e) => { setValue(e.target.value); }}
        />
      </label>
      <button
        type="button"
        onClick={submit}
        className="px-3 py-1 rounded bg-blue-600 hover:bg-blue-500 text-sm"
      >
        Add label
      </button>
    </div>
  );
}

function HostChecklist({ checked, onToggle }: {
  readonly checked: ReadonlySet<string>;
  readonly onToggle: (id: string) => void;
}) {
  const customer = useCustomerGate();
  const hosts = useHostOptions(customer);

  if (customer === null) {
    return <p className="text-sm text-gray-400">Pick a customer first</p>;
  }
  return (
    <fieldset aria-label="Hosts to label" className="max-h-56 overflow-auto rounded border border-gray-700 p-2">
      {hosts.map((host) => (
        <label key={host.id} className="flex items-center gap-2 py-0.5 text-sm text-gray-200">
          <input type="checkbox" checked={checked.has(host.id)} onChange={() => { onToggle(host.id); }} />
          <HostLabel name={host.name} online={host.online} />
        </label>
      ))}
    </fieldset>
  );
}

function AssignLabel({ labels }: { readonly labels: readonly DeviceTagLabel[] }) {
  const assignLabel = useDeviceTagsStore((s) => s.assignLabel);
  const [labelId, setLabelId] = useState('');
  const [checked, setChecked] = useState<ReadonlySet<string>>(new Set());
  const chosen = labels.some((l) => l.id === labelId) ? labelId : (labels.at(0)?.id ?? '');

  const toggle = (id: string) => {
    setChecked((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const submit = () => {
    if (!chosen || checked.size === 0) return;
    fireAndForget(assignLabel(chosen, [...checked]));
    setChecked(new Set());
  };

  return (
    <section className="mt-6 space-y-2">
      <h2 className="text-sm font-semibold text-gray-200">Give a label to hosts</h2>
      <label className="flex flex-col gap-1 w-64">
        <span className="text-xs uppercase text-gray-500 font-semibold">Label</span>
        <select
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm"
          value={chosen}
          onChange={(e) => { setLabelId(e.target.value); }}
        >
          {labels.map((label) => (
            <option key={label.id} value={label.id}>{label.key}={label.value}</option>
          ))}
        </select>
      </label>
      <HostChecklist checked={checked} onToggle={toggle} />
      <button
        type="button"
        onClick={submit}
        className="px-3 py-1 rounded bg-blue-600 hover:bg-blue-500 text-sm"
      >
        Assign
      </button>
    </section>
  );
}

/**
 * The server refuses to remove a label while a rule aims at it, since removal would widen a
 * threshold silently; the refusal is shown here.
 */
export function DeviceLabels() {
  const labels = useDeviceTagsStore((s) => s.labels);
  const assignments = useDeviceTagsStore((s) => s.assignments);
  const isLoading = useDeviceTagsStore((s) => s.isLoading);
  const error = useDeviceTagsStore((s) => s.error);
  const fetchTags = useDeviceTagsStore((s) => s.fetchTags);
  const deleteLabel = useDeviceTagsStore((s) => s.deleteLabel);
  const clearTag = useDeviceTagsStore((s) => s.clearTag);
  const canEdit = useAuthStore((s) => s.user?.is_admin ?? false);
  const customer = useOrganizationStore((s) => s.selectedOrganizationId);
  const hostNames = useHostNames(customer);
  const hostName = (id: string) => hostNames.get(id) ?? 'a removed host';

  useEffect(() => {
    fireAndForget(fetchTags());
  }, [fetchTags]);

  if (isLoading && labels.length === 0) return <LoadingSpinner />;

  const carrying = (key: string, value: string) =>
    assignments.filter((a) => Object.entries(a.tags).some(([k, v]) => k === key && v === value)).length;

  return (
    <div className="p-6">
      <Link to="/rules" className="text-xs text-blue-400 hover:text-blue-300">
        Rules
      </Link>
      <h1 className="text-xl font-bold mt-1">Labels</h1>
      <p className="text-sm text-gray-400 mb-4">
        Flat labels a rule can be aimed at. They cut across sites and customers rather than
        sitting on either, which is what makes &quot;the file servers&quot; something a threshold
        can be set for.
      </p>

      {error && (
        <p role="alert" className="mb-4 text-sm text-red-400">
          {error}
        </p>
      )}

      <table className="w-full bg-gray-800 border border-gray-700 rounded-lg overflow-hidden mb-4">
        <thead className="bg-gray-750">
          <tr>
            <th className={HEAD}>Label</th>
            <th className={HEAD}>Hosts carrying it</th>
            <th className={HEAD} aria-label="Actions" />
          </tr>
        </thead>
        <tbody>
          {labels.map((label) => (
            <tr key={label.id} className="border-t border-gray-700">
              <td className={CELL}>
                {label.key}={label.value}
              </td>
              <td className={`${CELL} tabular-nums`}>{carrying(label.key, label.value)}</td>
              <td className={CELL}>
                {canEdit && (
                  <span className="flex items-center gap-3">
                    <button
                      type="button"
                      onClick={() => { fireAndForget(deleteLabel(label.id)); }}
                      className="text-xs text-red-400 hover:text-red-300"
                    >
                      Remove
                    </button>
                  </span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {labels.length === 0 && (
        <p className="text-sm text-gray-400 mb-4">This customer has no labels yet.</p>
      )}

      {canEdit && <AddLabel />}
      {canEdit && labels.length > 0 && <AssignLabel labels={labels} />}

      <h2 className="text-sm font-semibold text-gray-200 mt-8 mb-2">Which host carries what</h2>
      <table className="w-full bg-gray-800 border border-gray-700 rounded-lg overflow-hidden">
        <thead className="bg-gray-750">
          <tr>
            <th className={HEAD}>Host</th>
            <th className={HEAD}>Labels</th>
          </tr>
        </thead>
        <tbody>
          {assignments.map((assignment) => (
            <tr key={assignment.device_id} className="border-t border-gray-700">
              <td className={CELL}>{hostName(assignment.device_id)}</td>
              <td className={CELL}>
                {Object.entries(assignment.tags)
                  .sort(([a], [b]) => a.localeCompare(b))
                  .map(([key, value]) => (
                    <span key={key} className="mr-3">
                      {key}={value}
                      {canEdit && (
                        <button
                          type="button"
                          aria-label={`Take ${key} off ${hostName(assignment.device_id)}`}
                          onClick={() => { fireAndForget(clearTag(assignment.device_id, key)); }}
                          className="ml-1 text-xs text-red-400 hover:text-red-300"
                        >
                          ×
                        </button>
                      )}
                    </span>
                  ))}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {assignments.length === 0 && (
        <p className="mt-2 text-sm text-gray-400">No host carries a label yet.</p>
      )}
    </div>
  );
}
