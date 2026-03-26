import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useSessionStore } from '../../state/session-store';
import { useAMTStore } from '../../state/amt-store';
import { useToastStore } from '../../state/toast-store';
import { StatusBadge } from './StatusBadge';
import type { components } from '../../types/api';

type PowerAction = components['schemas']['AMTPowerRequest']['action'];

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
  const amtDevices = useAMTStore((s) => s.amtDevices);
  const fetchAmtDevices = useAMTStore((s) => s.fetchAmtDevices);
  const sendPowerAction = useAMTStore((s) => s.sendPowerAction);
  const addToast = useToastStore((s) => s.addToast);
  const groups = useDeviceStore((s) => s.groups);
  const fetchGroups = useDeviceStore((s) => s.fetchGroups);
  const updateDeviceGroup = useDeviceStore((s) => s.updateDeviceGroup);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmPowerAction, setConfirmPowerAction] = useState<PowerAction | null>(null);
  const [selectedGroupId, setSelectedGroupId] = useState('');
  const [showAmtInstructions, setShowAmtInstructions] = useState(false);

  useEffect(() => {
    if (id) {
      fetchDevice(id);
      fetchSessions(id);
    }
    fetchAmtDevices();
    fetchGroups();
  }, [id, fetchDevice, fetchSessions, fetchAmtDevices, fetchGroups]);

  // Poll device data every 30s so agent_version and status stay in sync.
  useEffect(() => {
    if (!id) return;
    const interval = setInterval(() => fetchDevice(id), 30_000);
    return () => clearInterval(interval);
  }, [id, fetchDevice]);

  const amtDevice = device ? amtDevices.find((a) => a.hostname === device.hostname) : undefined;

  const handlePowerAction = async (action: PowerAction) => {
    const destructive = action === 'power_cycle' || action === 'hard_reset';
    if (destructive && confirmPowerAction !== action) {
      setConfirmPowerAction(action);
      return;
    }
    setConfirmPowerAction(null);
    if (!amtDevice) return;
    const ok = await sendPowerAction(amtDevice.uuid, action);
    if (ok) {
      addToast(`Power action "${action.replace('_', ' ')}" sent`, 'success');
    } else {
      addToast(`Failed to send power action`, 'error');
    }
  };

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

  const handleMoveGroup = async () => {
    if (!selectedGroupId || selectedGroupId === device.group_id) return;
    const ok = await updateDeviceGroup(device.id, selectedGroupId);
    if (ok) {
      addToast('Device moved to new group', 'success');
      setSelectedGroupId('');
    } else {
      addToast('Failed to move device', 'error');
    }
  };

  const handleStartSession = async () => {
    const result = await createSession(device.id);
    if (result) {
      navigate(`/sessions/${result.token}`, { state: { relayUrl: result.relay_url, capabilities: device.capabilities } });
    }
  };

  return (
    <div className="p-6 max-w-2xl">
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
          {device.agent_version && (
            <div>
              <dt className="text-gray-400">Agent Version</dt>
              <dd>{device.agent_version}</dd>
            </div>
          )}
        </dl>

        {groups.length > 1 && (
          <div>
            <h3 className="text-sm font-semibold text-gray-300 mb-2">Move to Group</h3>
            <div className="flex gap-2">
              <select
                value={selectedGroupId}
                onChange={(e) => setSelectedGroupId(e.target.value)}
                className="bg-gray-900 border border-gray-600 rounded px-3 py-1.5 text-sm flex-1"
              >
                <option value="">Select group...</option>
                {groups.filter((g) => g.id !== device.group_id).map((g) => (
                  <option key={g.id} value={g.id}>{g.name}</option>
                ))}
              </select>
              <button
                type="button"
                onClick={handleMoveGroup}
                disabled={!selectedGroupId}
                className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 rounded text-sm disabled:opacity-50"
              >
                Move
              </button>
            </div>
          </div>
        )}

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

        {amtDevice ? (
          <div>
            <h3 className="text-sm font-semibold text-gray-300 mb-2">AMT Power Actions</h3>
            <p className="text-xs text-gray-500 mb-2">
              AMT Status: <span className={amtDevice.status === 'online' ? 'text-green-400' : 'text-red-400'}>{amtDevice.status === 'online' ? 'Online' : 'Offline'}</span>
              {amtDevice.model && <> &middot; {amtDevice.model}</>}
            </p>
            <div className="flex gap-2 flex-wrap">
              <button type="button" onClick={() => handlePowerAction('power_on')} className="px-3 py-1 bg-green-700 hover:bg-green-600 rounded text-xs">
                Power On
              </button>
              <button type="button" onClick={() => handlePowerAction('soft_off')} className="px-3 py-1 bg-yellow-700 hover:bg-yellow-600 rounded text-xs">
                Soft Off
              </button>
              <button type="button" onClick={() => handlePowerAction('power_cycle')} className="px-3 py-1 bg-orange-700 hover:bg-orange-600 rounded text-xs">
                {confirmPowerAction === 'power_cycle' ? 'Confirm Cycle' : 'Power Cycle'}
              </button>
              <button type="button" onClick={() => handlePowerAction('hard_reset')} className="px-3 py-1 bg-red-700 hover:bg-red-600 rounded text-xs">
                {confirmPowerAction === 'hard_reset' ? 'Confirm Reset' : 'Hard Reset'}
              </button>
            </div>
          </div>
        ) : (
          <div>
            <button
              type="button"
              onClick={() => setShowAmtInstructions(!showAmtInstructions)}
              className="text-sm font-semibold text-gray-300 flex items-center gap-2"
            >
              <span className={`text-xs transition-transform ${showAmtInstructions ? 'rotate-90' : ''}`}>
                &#9654;
              </span>
              Intel AMT Setup
            </button>
            {showAmtInstructions && (
              <div className="mt-2 bg-gray-900 border border-gray-700 rounded-lg p-4 text-sm text-gray-400 space-y-3">
                <p>
                  Intel AMT (Active Management Technology) enables out-of-band power management
                  for supported hardware. To enable AMT for this device:
                </p>
                <ol className="list-decimal list-inside space-y-2">
                  <li>
                    <strong className="text-gray-300">Enable AMT in BIOS</strong> — Enter the BIOS/UEFI
                    setup (usually F2/Del at boot) and enable Intel AMT / ME (Management Engine).
                  </li>
                  <li>
                    <strong className="text-gray-300">Configure MEBx</strong> — Press Ctrl+P at boot to
                    enter MEBx. Set a strong password and configure the network settings (DHCP or static IP).
                  </li>
                  <li>
                    <strong className="text-gray-300">Enable remote access</strong> — In MEBx, enable
                    &quot;Remote Setup And Configuration&quot; and ensure the AMT network interface is active.
                  </li>
                  <li>
                    <strong className="text-gray-300">Verify connectivity</strong> — The device will
                    automatically register with the MPS server once AMT is configured and the network is
                    reachable. Power actions will appear here once connected.
                  </li>
                </ol>
                <p className="text-xs text-gray-500">
                  Requires Intel vPro-compatible hardware with AMT firmware. Not all Intel processors support AMT.
                </p>
              </div>
            )}
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
