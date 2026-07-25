import { useState } from 'react';
import { useDeviceStore } from './state/device-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { UNGROUPED_GROUP_ID, isDeviceDrag, readDraggedDeviceId } from './device-drag';
import { fireAndForget } from '../../lib/fire-and-forget';

/** A device with no real group: an empty id or the all-zeros placeholder UUID. */
function isUngrouped(id: string | undefined | null): boolean {
  const trimmed = id?.trim();
  return !trimmed || trimmed === UNGROUPED_GROUP_ID;
}

export function GroupSidebar() {
  const groups = useDeviceStore((s) => s.groups);
  const devices = useDeviceStore((s) => s.devices);
  const selectedGroupId = useDeviceStore((s) => s.selectedGroupId);
  const selectGroup = useDeviceStore((s) => s.selectGroup);
  const createGroup = useDeviceStore((s) => s.createGroup);
  const deleteGroup = useDeviceStore((s) => s.deleteGroup);
  const updateDeviceGroup = useDeviceStore((s) => s.updateDeviceGroup);
  const fetchDevices = useDeviceStore((s) => s.fetchDevices);
  const addToast = useToastStore((s) => s.addToast);
  const [newName, setNewName] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  // Which zone the pointer is over mid-drag, so exactly one row lights up.
  const [dropZone, setDropZone] = useState<string | null>(null);

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

  const handleDrop = async (transfer: DataTransfer, targetId: string, targetName: string) => {
    const deviceId = readDraggedDeviceId(transfer);
    if (!deviceId) return;
    const dragged = devices.find((d) => d.id === deviceId);
    const currentId = isUngrouped(dragged?.group_id) ? UNGROUPED_GROUP_ID : (dragged?.group_id ?? '');
    if (currentId === targetId) return;

    const label = dragged?.hostname ?? 'device';
    const ok = await updateDeviceGroup(deviceId, targetId);
    if (!ok) {
      addToast(`Failed to move ${label} to ${targetName}`, 'error');
      return;
    }
    addToast(`Moved ${label} to ${targetName}`, 'success');
    // Re-pull under the active filter so a device dragged out of the visible
    // group leaves the grid instead of lingering as a stale card.
    await fetchDevices(selectedGroupId ?? undefined);
  };

  /** Drop-zone wiring shared by every group row and the Ungrouped zone. */
  const dropProps = (targetId: string, targetName: string) => ({
    role: 'listitem',
    'aria-label': targetName,
    onDragOver: (e: React.DragEvent) => {
      if (!isDeviceDrag(e.dataTransfer)) return;
      e.preventDefault(); // signals "this is a valid drop target"
      e.dataTransfer.dropEffect = 'move';
      setDropZone(targetId);
    },
    onDragLeave: () => { setDropZone((z) => (z === targetId ? null : z)); },
    onDrop: (e: React.DragEvent) => {
      e.preventDefault();
      setDropZone(null);
      fireAndForget(handleDrop(e.dataTransfer, targetId, targetName));
    },
  });

  const zoneRing = (targetId: string) => (dropZone === targetId ? 'ring-2 ring-blue-400' : '');

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

      <div role="list" className="space-y-2">
        {groups.map((group) => (
          <div
            key={group.id}
            {...dropProps(group.id, group.name)}
            className={`flex items-center justify-between rounded px-3 py-2 cursor-pointer text-sm ${zoneRing(group.id)} ${
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

        {groups.length > 0 && (
          <div
            {...dropProps(UNGROUPED_GROUP_ID, 'Ungrouped')}
            title="Drop a device here to take it out of its group"
            className={`rounded border border-dashed border-gray-600 px-3 py-2 text-xs text-gray-500 ${zoneRing(UNGROUPED_GROUP_ID)}`}
          >
            Ungrouped
          </div>
        )}
      </div>

      {groups.length === 0 && (
        <p className="text-sm text-gray-500">No groups yet</p>
      )}

      {groups.length > 0 && (
        <p className="text-xs text-gray-600 pt-1">Drag a device card onto a group to move it.</p>
      )}
    </div>
  );
}
