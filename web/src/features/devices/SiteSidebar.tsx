import { useState } from 'react';
import { useDeviceStore } from './state/device-store';
import { useAuthStore } from '../../state/auth-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { UNFILED_SITE_ID, isDeviceDrag, readDraggedDeviceId } from './device-drag';
import { fireAndForget } from '../../lib/fire-and-forget';

/** A device with no real site: an empty id or the all-zeros placeholder UUID. */
function isUnfiled(id: string | undefined | null): boolean {
  const trimmed = id?.trim();
  return !trimmed || trimmed === UNFILED_SITE_ID;
}

export function SiteSidebar() {
  const sites = useDeviceStore((s) => s.sites);
  const devices = useDeviceStore((s) => s.devices);
  const selectedSiteId = useDeviceStore((s) => s.selectedSiteId);
  const selectSite = useDeviceStore((s) => s.selectSite);
  const createSite = useDeviceStore((s) => s.createSite);
  const deleteSite = useDeviceStore((s) => s.deleteSite);
  const updateDeviceSite = useDeviceStore((s) => s.updateDeviceSite);
  const fetchDevices = useDeviceStore((s) => s.fetchDevices);
  const addToast = useToastStore((s) => s.addToast);
  // Creating a site, deleting one, and dragging a device between them are all
  // configuration changes the server refuses for a non-admin, so a non-admin
  // gets a read-only sidebar rather than controls that fail on click.
  const isAdmin = useAuthStore((s) => s.user?.is_admin ?? false);
  const [newName, setNewName] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  // Which zone the pointer is over mid-drag, so exactly one row lights up.
  const [dropZone, setDropZone] = useState<string | null>(null);

  const handleCreate = async (e: React.SyntheticEvent) => {
    e.preventDefault();
    if (!newName.trim()) return;
    await createSite(newName.trim());
    setNewName('');
    setShowForm(false);
  };

  const handleDelete = async (id: string) => {
    if (confirmDelete === id) {
      await deleteSite(id);
      setConfirmDelete(null);
    } else {
      setConfirmDelete(id);
    }
  };

  const handleDrop = async (transfer: DataTransfer, targetId: string, targetName: string) => {
    const deviceId = readDraggedDeviceId(transfer);
    if (!deviceId) return;
    const dragged = devices.find((d) => d.id === deviceId);
    const currentId = isUnfiled(dragged?.site_id) ? UNFILED_SITE_ID : (dragged?.site_id ?? '');
    if (currentId === targetId) return;

    const label = dragged?.hostname ?? 'device';
    const ok = await updateDeviceSite(deviceId, targetId);
    if (!ok) {
      addToast(`Failed to move ${label} to ${targetName}`, 'error');
      return;
    }
    addToast(`Moved ${label} to ${targetName}`, 'success');
    // Re-pull under the active filter so a device dragged out of the visible
    // site leaves the grid instead of lingering as a stale card.
    await fetchDevices(selectedSiteId ?? undefined);
  };

  /** Drop-zone wiring shared by every site row and the Unfiled zone. */
  const dropProps = (targetId: string, targetName: string) => (!isAdmin ? {
    role: 'listitem',
    'aria-label': targetName,
  } : {
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
        <h2 className="text-sm font-semibold text-gray-300 uppercase">Sites</h2>
        {isAdmin && (
          <button
            type="button"
            onClick={() => setShowForm(!showForm)}
            className="text-sm text-blue-400 hover:text-blue-300"
          >
            {showForm ? 'Cancel' : '+ New'}
          </button>
        )}
      </div>

      {isAdmin && showForm && (
        <form onSubmit={(e) => { fireAndForget(handleCreate(e)); }} className="flex gap-2 mb-2">
          <input
            type="text"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="Site name"
            className="flex-1 px-2 py-1 text-sm bg-gray-700 border border-gray-600 rounded text-white"
          />
          <button type="submit" className="px-2 py-1 text-sm bg-blue-600 rounded hover:bg-blue-700">
            Add
          </button>
        </form>
      )}

      <div role="list" className="space-y-2">
        {sites.map((site) => (
          <div
            key={site.id}
            {...dropProps(site.id, site.name)}
            className={`flex items-center justify-between rounded px-3 py-2 cursor-pointer text-sm ${zoneRing(site.id)} ${
              selectedSiteId === site.id ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-750 hover:text-gray-200'
            }`}
          >
            <button
              type="button"
              onClick={() => selectSite(site.id)}
              className="flex-1 text-left truncate"
            >
              {site.name}
            </button>
            {isAdmin && (
              <button
                type="button"
                onClick={() => { fireAndForget(handleDelete(site.id)); }}
                className="ml-2 text-xs text-gray-500 hover:text-red-400"
                title={confirmDelete === site.id ? 'Click again to confirm' : 'Delete site'}
              >
                {confirmDelete === site.id ? 'Confirm?' : 'x'}
              </button>
            )}
          </div>
        ))}

        {isAdmin && sites.length > 0 && (
          <div
            {...dropProps(UNFILED_SITE_ID, 'Unfiled')}
            title="Drop a device here to take it out of its site"
            className={`rounded border border-dashed border-gray-600 px-3 py-2 text-xs text-gray-500 ${zoneRing(UNFILED_SITE_ID)}`}
          >
            Unfiled
          </div>
        )}
      </div>

      {sites.length === 0 && (
        <p className="text-sm text-gray-500">No sites yet</p>
      )}

      {isAdmin && sites.length > 0 && (
        <p className="text-xs text-gray-600 pt-1">Drag a device card onto a site to move it.</p>
      )}
    </div>
  );
}
