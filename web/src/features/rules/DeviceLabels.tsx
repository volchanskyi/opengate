import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { fireAndForget } from '../../lib/fire-and-forget';
import { LoadingSpinner } from '../../components/LoadingSpinner';
import { useAuthStore } from '../../state/auth-store';
import { useDeviceTagsStore } from './state/device-tags-store';

const CELL = 'px-3 py-2 text-sm text-gray-300';
const HEAD = 'px-3 py-2 text-left text-xs font-semibold text-gray-400';

/** Adding an entry to the list, which is the only way a label comes into being. */
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

/** Giving one label to several machines at once. */
function BulkAssign({ labelId }: { readonly labelId: string }) {
  const assignLabel = useDeviceTagsStore((s) => s.assignLabel);
  const [machines, setMachines] = useState('');

  const submit = () => {
    const ids = machines.split(/[\s,]+/).filter((id) => id.length > 0);
    if (ids.length === 0) return;
    fireAndForget(assignLabel(labelId, ids));
    setMachines('');
  };

  return (
    <span className="flex items-center gap-2">
      <input
        className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-xs w-64"
        aria-label="Machines to label"
        placeholder="machine ids"
        value={machines}
        onChange={(e) => { setMachines(e.target.value); }}
      />
      <button
        type="button"
        onClick={submit}
        className="px-2 py-0.5 rounded bg-gray-700 hover:bg-gray-600 text-xs"
      >
        Assign
      </button>
    </span>
  );
}

/**
 * The labels a customer's machines are picked out by.
 *
 * A label cuts across the tenancy ladder: `role=file-server` describes machines
 * in four offices, which is the set a threshold is usually meant for and the set
 * no rung names. Removing one is not a free action — a rule aimed at it loses a
 * tuned value on every machine that carried it, which widens a threshold across
 * an estate without anything saying so — so the server refuses it while a rule
 * still aims at it, and the refusal is shown here rather than swallowed.
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
        Flat labels a rule can be aimed at. They cut across offices and customers rather than
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
            <th className={HEAD}>Machines carrying it</th>
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
                    <BulkAssign labelId={label.id} />
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

      <h2 className="text-sm font-semibold text-gray-200 mt-8 mb-2">Which machine carries what</h2>
      <table className="w-full bg-gray-800 border border-gray-700 rounded-lg overflow-hidden">
        <thead className="bg-gray-750">
          <tr>
            <th className={HEAD}>Machine</th>
            <th className={HEAD}>Labels</th>
          </tr>
        </thead>
        <tbody>
          {assignments.map((assignment) => (
            <tr key={assignment.device_id} className="border-t border-gray-700">
              <td className={CELL}>{assignment.device_id}</td>
              <td className={CELL}>
                {Object.entries(assignment.tags)
                  .sort(([a], [b]) => a.localeCompare(b))
                  .map(([key, value]) => (
                    <span key={key} className="mr-3">
                      {key}={value}
                      {canEdit && (
                        <button
                          type="button"
                          aria-label={`Take ${key} off ${assignment.device_id}`}
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
        <p className="mt-2 text-sm text-gray-400">No machine carries a label yet.</p>
      )}
    </div>
  );
}
