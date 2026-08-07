import { useEffect, useState, type FormEvent } from 'react';
import { useOrganizationStore } from './state/organization-store';
import { fireAndForget } from '../../lib/fire-and-forget';

/**
 * Manages the tenant's customers: take one on, rename it, retire it, or delete
 * it and everything under it.
 *
 * Deleting is destructive in a way renaming and archiving are not — it takes the
 * customer's devices and their history with it — so it asks first, and the
 * server refuses the tenant's last customer regardless.
 */
export function OrganizationManagement() {
  const organizations = useOrganizationStore((s) => s.organizations);
  const error = useOrganizationStore((s) => s.error);
  const fetchOrganizations = useOrganizationStore((s) => s.fetchOrganizations);
  const createOrganization = useOrganizationStore((s) => s.createOrganization);
  const renameOrganization = useOrganizationStore((s) => s.renameOrganization);
  const setOrganizationArchived = useOrganizationStore((s) => s.setOrganizationArchived);
  const deleteOrganization = useOrganizationStore((s) => s.deleteOrganization);

  const [newName, setNewName] = useState('');
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingName, setEditingName] = useState('');
  const [showArchived, setShowArchived] = useState(false);

  useEffect(() => {
    fireAndForget(fetchOrganizations(showArchived));
  }, [fetchOrganizations, showArchived]);

  async function onCreate(e: FormEvent) {
    e.preventDefault();
    if (newName.trim() === '') return;
    if (await createOrganization(newName.trim())) setNewName('');
  }

  async function onRename(id: string) {
    if (editingName.trim() !== '' && (await renameOrganization(id, editingName.trim()))) {
      setEditingId(null);
    }
  }

  async function onDelete(id: string, name: string) {
    if (!globalThis.confirm(`Delete ${name} and every device belonging to it? This cannot be undone.`)) {
      return;
    }
    await deleteOrganization(id);
  }

  return (
    <div className="max-w-3xl">
      <h1 className="text-xl font-semibold mb-1">Customers</h1>
      <p className="text-sm text-gray-400 mb-4">
        Each device belongs to one customer. Deleting a customer deletes its devices and their
        history.
      </p>

      <form onSubmit={(e) => fireAndForget(onCreate(e))} className="flex gap-2 mb-6">
        <input
          aria-label="New customer name"
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          placeholder="Customer name"
          className="flex-1 bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm"
        />
        <button
          type="submit"
          className="bg-blue-600 hover:bg-blue-500 rounded px-4 py-2 text-sm font-medium"
        >
          Add customer
        </button>
      </form>

      {error && <p className="text-sm text-red-400 mb-4">{error}</p>}

      <label className="flex items-center gap-2 text-sm text-gray-400 mb-3">
        <input
          type="checkbox"
          checked={showArchived}
          onChange={(e) => setShowArchived(e.target.checked)}
        />
        Show retired customers
      </label>

      <ul className="divide-y divide-gray-700 border border-gray-700 rounded">
        {organizations.map((org) => (
          <li key={org.id} className="flex items-center gap-3 px-4 py-3">
            {editingId === org.id ? (
              <>
                <input
                  aria-label={`Rename ${org.name}`}
                  value={editingName}
                  onChange={(e) => setEditingName(e.target.value)}
                  className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm"
                />
                <button
                  onClick={() => fireAndForget(onRename(org.id))}
                  className="text-sm text-blue-400 hover:text-blue-300"
                >
                  Save
                </button>
                <button
                  onClick={() => setEditingId(null)}
                  className="text-sm text-gray-400 hover:text-white"
                >
                  Cancel
                </button>
              </>
            ) : (
              <>
                <span className="flex-1 text-sm">
                  {org.name}
                  {org.archived_at && <span className="ml-2 text-xs text-gray-500">Retired</span>}
                </span>
                <button
                  onClick={() => {
                    setEditingId(org.id);
                    setEditingName(org.name);
                  }}
                  className="text-sm text-gray-400 hover:text-white"
                >
                  Rename
                </button>
                <button
                  onClick={() =>
                    fireAndForget(setOrganizationArchived(org.id, org.archived_at == null))
                  }
                  className="text-sm text-gray-400 hover:text-white"
                >
                  {org.archived_at ? 'Restore' : 'Retire'}
                </button>
                <button
                  onClick={() => fireAndForget(onDelete(org.id, org.name))}
                  className="text-sm text-red-400 hover:text-red-300"
                >
                  Delete
                </button>
              </>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
