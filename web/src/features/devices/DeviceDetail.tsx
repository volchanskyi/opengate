import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useSessionStore } from '../../state/session-store';
import { useAMTStore } from '../../state/amt-store';
import { useUpdateStore } from '../../state/update-store';
import { useToastStore } from '../../state/toast-store';
import { StatusBadge } from './StatusBadge';
import { DeviceLogs } from './DeviceLogs';
import type { components } from '../../types/api';

type PowerAction = components['schemas']['AMTPowerRequest']['action'];

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / Math.pow(1024, i);
  return `${val.toFixed(val >= 100 ? 0 : 1)} ${units[i]}`;
}

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
  const restartAgent = useDeviceStore((s) => s.restartAgent);
  const hardware = useDeviceStore((s) => s.hardware);
  const fetchHardware = useDeviceStore((s) => s.fetchHardware);
  const upgradeAgent = useDeviceStore((s) => s.upgradeAgent);
  const manifests = useUpdateStore((s) => s.manifests);
  const fetchManifests = useUpdateStore((s) => s.fetchManifests);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmRestart, setConfirmRestart] = useState(false);
  const [isRestarting, setIsRestarting] = useState(false);
  const [isUpgrading, setIsUpgrading] = useState(false);
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
    fetchManifests();
  }, [id, fetchDevice, fetchSessions, fetchAmtDevices, fetchGroups, fetchManifests]);

  // Poll device data every 30s so agent_version and status stay in sync.
  useEffect(() => {
    if (!id) return;
    const interval = setInterval(() => fetchDevice(id), 30_000);
    return () => clearInterval(interval);
  }, [id, fetchDevice]);

  const amtDevice = device ? amtDevices.find((a) => a.hostname === device.hostname) : undefined;

  // Find the latest manifest matching this device's OS.
  const latestManifest = device
    ? manifests
        .filter((m) => m.os === device.os)
        .sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true }))[0]
    : undefined;

  const isUpToDate = !!(
    latestManifest &&
    device?.agent_version &&
    device.agent_version.localeCompare(latestManifest.version, undefined, { numeric: true }) >= 0
  );

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

  const handleRestart = async () => {
    if (sessions.length > 0 && !confirmRestart) {
      setConfirmRestart(true);
      return;
    }
    setConfirmRestart(false);
    setIsRestarting(true);
    const ok = await restartAgent(device.id);
    if (ok) {
      addToast('Restart command sent', 'success');
    } else {
      addToast('Failed to restart agent', 'error');
    }
    setIsRestarting(false);
  };

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
    } else {
      addToast('Failed to start session — agent may be offline or restarting', 'error');
    }
  };

  const handleUpgrade = async () => {
    if (!latestManifest) return;
    setIsUpgrading(true);
    const ok = await upgradeAgent(device.id, latestManifest.version, latestManifest.os, latestManifest.arch);
    if (ok) {
      addToast(`Upgrade to v${latestManifest.version} pushed`, 'success');
    } else {
      addToast('Failed to push upgrade', 'error');
    }
    setIsUpgrading(false);
  };

  return (
    <div className="p-6 grid grid-cols-1 lg:grid-cols-2 gap-4 items-start">
      {/* Device Detail Card */}
      <div className="bg-gray-800 border border-gray-700 rounded-lg p-6 space-y-4">
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-3">
            <h2 className="text-xl font-bold">{device.hostname}</h2>
            <StatusBadge status={device.status} />
          </div>
          <div className="flex gap-2 flex-wrap justify-end">
            <button
              type="button"
              onClick={handleStartSession}
              className="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 rounded text-xs font-medium"
            >
              Start Session
            </button>
            <button
              type="button"
              onClick={handleRestart}
              disabled={device.status !== 'online' || isRestarting}
              className="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 rounded text-xs font-medium disabled:opacity-50"
            >
              {isRestarting
                ? 'Restarting...'
                : confirmRestart
                  ? `Confirm (${sessions.length} active)`
                  : 'Restart Agent'}
            </button>
            {latestManifest && !isUpToDate && (
              <button
                type="button"
                onClick={handleUpgrade}
                disabled={isUpgrading || device.status !== 'online'}
                className="px-3 py-1.5 bg-green-600 hover:bg-green-700 rounded text-xs font-medium disabled:opacity-50"
              >
                {isUpgrading ? 'Upgrading...' : `Upgrade to v${latestManifest.version}`}
              </button>
            )}
            {isUpToDate && (
              <span className="px-3 py-1.5 bg-gray-700 text-gray-400 rounded text-xs font-medium">
                Up to date
              </span>
            )}
            <button
              type="button"
              onClick={handleDelete}
              className="px-3 py-1.5 bg-red-600 hover:bg-red-700 rounded text-xs font-medium"
            >
              {confirmDelete ? 'Confirm Delete' : 'Delete Device'}
            </button>
          </div>
        </div>

        <dl className="grid grid-cols-2 gap-3 text-sm">
          <div>
            <dt className="text-gray-400">OS</dt>
            <dd>{device.os_display || device.os}</dd>
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

        <div>
          <div className="flex items-center justify-between mb-2">
            <h3 className="text-sm font-semibold text-gray-300">Hardware</h3>
            <button
              type="button"
              onClick={() => id && fetchHardware(id)}
              className="px-3 py-1 bg-blue-600 hover:bg-blue-500 rounded text-xs font-medium"
            >
              Refresh Hardware
            </button>
          </div>
          {hardware && (
            <>
              <dl className="grid grid-cols-2 gap-3 text-sm">
                <div>
                  <dt className="text-gray-400">CPU</dt>
                  <dd>{hardware.cpu_model} ({hardware.cpu_cores} cores)</dd>
                </div>
                <div>
                  <dt className="text-gray-400">RAM</dt>
                  <dd>{formatBytes(hardware.ram_total_mb * 1024 * 1024)}</dd>
                </div>
                <div>
                  <dt className="text-gray-400">Disk</dt>
                  <dd>{formatBytes(hardware.disk_free_mb * 1024 * 1024)} free / {formatBytes(hardware.disk_total_mb * 1024 * 1024)}</dd>
                </div>
                <div>
                  <dt className="text-gray-400">Last Updated</dt>
                  <dd>{new Date(hardware.updated_at).toLocaleString()}</dd>
                </div>
              </dl>
              {hardware.network_interfaces.length > 0 && (
                <div className="mt-2">
                  <h4 className="text-xs text-gray-400 mb-1">Network Interfaces</h4>
                  <ul className="text-xs space-y-1">
                    {hardware.network_interfaces.map((ni) => (
                      <li key={ni.name} className="font-mono">
                        {ni.name}: {ni.mac}{ni.ipv4.length > 0 && ` — ${ni.ipv4.join(', ')}`}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </>
          )}
        </div>
      </div>

      {/* Agent Logs Card (separate tile, right side) */}
      <div className="bg-gray-800 border border-gray-700 rounded-lg p-6">
        <DeviceLogs deviceId={device.id} />
      </div>
    </div>
  );
}
