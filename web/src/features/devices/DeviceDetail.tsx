import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useSessionStore } from '../../state/session-store';
import { StatusBadge } from './StatusBadge';

export function DeviceDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const device = useDeviceStore((s) => s.selectedDevice);
  const isLoading = useDeviceStore((s) => s.isLoading);
  const fetchDevice = useDeviceStore((s) => s.fetchDevice);
  const deleteDevice = useDeviceStore((s) => s.deleteDevice);
  const sessions = useSessionStore((s) => s.sessions);
  const fetchSessions = useSessionStore((s) => s.fetchSessions);
  const createSession = useSessionStore((s) => s.createSession);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    if (id) {
      fetchDevice(id);
      fetchSessions(id);
    }
  }, [id, fetchDevice, fetchSessions]);

  if (isLoading || !device) {
    return (
      <div className="p-6">
        <div className="animate-pulse space-y-4">
          <div className="h-6 bg-gray-700 rounded w-1/4" />
          <div className="h-4 bg-gray-700 rounded w-1/2" />
          <div className="h-4 bg-gray-700 rounded w-1/3" />
        </div>
      </div>
    );
  }

  const handleDelete = async () => {
    if (!confirmDelete) {
      setConfirmDelete(true);
      return;
    }
    await deleteDevice(device.id);
    navigate('/devices');
  };

  const handleStartSession = async () => {
    const result = await createSession(device.id);
    if (result) {
      navigate(`/sessions/${result.token}`, { state: { relayUrl: result.relay_url } });
    }
  };

  return (
    <div className="p-6 max-w-2xl">
      <button
        type="button"
        onClick={() => navigate('/devices')}
        className="text-sm text-gray-400 hover:text-white mb-4 inline-block"
      >
        &larr; Back to devices
      </button>

      <div className="bg-gray-800 border border-gray-700 rounded-lg p-6 space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-xl font-bold">{device.hostname}</h2>
          <StatusBadge status={device.status} />
        </div>

        <dl className="grid grid-cols-2 gap-3 text-sm">
          <div>
            <dt className="text-gray-400">OS</dt>
            <dd>{device.os}</dd>
          </div>
          <div>
            <dt className="text-gray-400">Group ID</dt>
            <dd className="font-mono text-xs">{device.group_id}</dd>
          </div>
          <div>
            <dt className="text-gray-400">Last Seen</dt>
            <dd>{new Date(device.last_seen).toLocaleString()}</dd>
          </div>
          <div>
            <dt className="text-gray-400">Created</dt>
            <dd>{new Date(device.created_at).toLocaleString()}</dd>
          </div>
        </dl>

        {sessions.length > 0 && (
          <div>
            <h3 className="text-sm font-semibold text-gray-300 mb-2">Active Sessions ({sessions.length})</h3>
            <ul className="space-y-1">
              {sessions.map((s) => (
                <li key={s.token} className="text-xs text-gray-400 font-mono truncate">{s.token}</li>
              ))}
            </ul>
          </div>
        )}

        <div className="flex gap-3 pt-2">
          <button
            type="button"
            onClick={handleStartSession}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-sm font-medium"
          >
            Start Session
          </button>
          <button
            type="button"
            onClick={handleDelete}
            className="px-4 py-2 bg-red-600 hover:bg-red-700 rounded text-sm font-medium"
          >
            {confirmDelete ? 'Confirm Delete' : 'Delete Device'}
          </button>
        </div>
      </div>
    </div>
  );
}
