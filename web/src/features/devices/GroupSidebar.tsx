import { useState } from 'react';
import { useDeviceStore } from '../../state/device-store';
import { fireAndForget } from '../../lib/fire-and-forget';

export function GroupSidebar() {
  const groups = useDeviceStore((s) => s.groups);
  const selectedGroupId = useDeviceStore((s) => s.selectedGroupId);
  const selectGroup = useDeviceStore((s) => s.selectGroup);
  const createGroup = useDeviceStore((s) => s.createGroup);
  const deleteGroup = useDeviceStore((s) => s.deleteGroup);
  const [newName, setNewName] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  const handleCreate = async (e: React.SyntheticEvent) => {
    e.preventDefault();
    if (!newName.trim()) return;
    await createGroup(newName.trim());
    setNewName('');
    setShowForm(false);
  };

  const handleDelete = async (id: string) => {
    if (confirmDelete === id) {
      await deleteGroup(id);
      setConfirmDelete(null);
    } else {
      setConfirmDelete(id);
    }
  };

  return (
    <div className="w-64 bg-gray-800 border-r border-gray-700 p-4 space-y-2">
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-semibold text-gray-300 uppercase">Groups</h2>
        <button
          type="button"
          onClick={() => setShowForm(!showForm)}
          className="text-sm text-blue-400 hover:text-blue-300"
        >
          {showForm ? 'Cancel' : '+ New'}
        </button>
      </div>

      {showForm && (
        <form onSubmit={(e) => { fireAndForget(handleCreate(e)); }} className="flex gap-2 mb-2">
          <input
            type="text"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="Group name"
            className="flex-1 px-2 py-1 text-sm bg-gray-700 border border-gray-600 rounded text-white"
          />
          <button type="submit" className="px-2 py-1 text-sm bg-blue-600 rounded hover:bg-blue-700">
            Add
          </button>
        </form>
      )}

      {groups.map((group) => (
        <div
          key={group.id}
          className={`flex items-center justify-between rounded px-3 py-2 cursor-pointer text-sm ${
            selectedGroupId === group.id ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-750 hover:text-gray-200'
          }`}
        >
          <button
            type="button"
            onClick={() => selectGroup(group.id)}
            className="flex-1 text-left truncate"
          >
            {group.name}
          </button>
          <button
            type="button"
            onClick={() => { fireAndForget(handleDelete(group.id)); }}
            className="ml-2 text-xs text-gray-500 hover:text-red-400"
            title={confirmDelete === group.id ? 'Click again to confirm' : 'Delete group'}
          >
            {confirmDelete === group.id ? 'Confirm?' : 'x'}
          </button>
        </div>
      ))}

      {groups.length === 0 && (
        <p className="text-sm text-gray-500">No groups yet</p>
      )}
    </div>
  );
}
