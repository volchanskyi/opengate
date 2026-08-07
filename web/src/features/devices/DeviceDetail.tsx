import { useEffect, useRef, useState } from 'react';
import { useParams, useNavigate } from 'react-router';
import { useDeviceStore } from './state/device-store';
import { useAuthStore } from '../../state/auth-store';
import { useSessionStore } from '../session';
import { useOrganizationStore } from '../organizations';
import { useUpdateStore } from './state/update-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { StatusBadge } from './StatusBadge';
import { AmtBadge } from './AmtBadge';
import { MaintenanceBadge } from './MaintenanceBadge';
import { MaintenancePanel } from './MaintenancePanel';
import { DeviceLogs } from './DeviceLogs';
import { SystemLogs } from './SystemLogs';
import { DeviceMetrics } from './DeviceMetrics';
import { DeviceInventory } from './DeviceInventory';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useVisibleInterval } from '../../lib/use-visible-interval';
import { PlayIcon, RestartIcon, SpinnerIcon, CheckIcon, TrashIcon } from '../../components/icons';

/** How often the detail page re-reads the device and its sessions while visible. */
const DEVICE_DETAIL_POLL_MS = 30_000;

type PowerAction = components['schemas']['AMTPowerRequest']['action'];
type DeviceAMT = components['schemas']['DeviceAMT'];

interface AmtSectionProps {
  readonly amt: DeviceAMT | undefined;
  readonly confirmPowerAction: PowerAction | null;
  readonly onPowerAction: (action: PowerAction) => void;
}

/**
 * Out-of-band power controls. They need a live CIRA tunnel, so they appear only
 * once the device's AMT connection is linked *and* online — the badge beside the
 * hostname is what tells an operator AMT exists at all. Setup instructions are
 * static BIOS/MEBx documentation and live on the /setup page.
 */
function AmtSection({ amt, confirmPowerAction, onPowerAction }: AmtSectionProps) {
  if (!amt?.uuid || amt.status !== 'online') return null;

  return (
    <div>
      <h3 className="text-sm font-semibold text-gray-300 mb-2">AMT Power Actions</h3>
      <div className="flex gap-2 flex-wrap">
        <button type="button" onClick={() => { onPowerAction('power_on'); }} className="px-3 py-1 bg-green-700 hover:bg-green-600 rounded text-xs">
          Power On
        </button>
        <button type="button" onClick={() => { onPowerAction('soft_off'); }} className="px-3 py-1 bg-yellow-700 hover:bg-yellow-600 rounded text-xs">
          Soft Off
        </button>
        <button type="button" onClick={() => { onPowerAction('power_cycle'); }} className="px-3 py-1 bg-orange-700 hover:bg-orange-600 rounded text-xs">
          {confirmPowerAction === 'power_cycle' ? 'Confirm Cycle' : 'Power Cycle'}
        </button>
        <button type="button" onClick={() => { onPowerAction('hard_reset'); }} className="px-3 py-1 bg-red-700 hover:bg-red-600 rounded text-xs">
          {confirmPowerAction === 'hard_reset' ? 'Confirm Reset' : 'Hard Reset'}
        </button>
      </div>
    </div>
  );
}

const UNASSIGNED_GROUP_ID = '00000000-0000-0000-0000-000000000000';

/** A device with no real group: an empty id or the all-zeros placeholder UUID. */
function isUnassignedGroup(id: string | undefined | null): boolean {
  const trimmed = id?.trim();
  return !trimmed || trimmed === UNASSIGNED_GROUP_ID;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const idx = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const val = bytes / Math.pow(1024, idx);
  return `${val.toFixed(val >= 100 ? 0 : 1)} ${units.at(idx) ?? 'B'}`;
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
  const sendPowerAction = useDeviceStore((s) => s.sendPowerAction);
  const addToast = useToastStore((s) => s.addToast);
  const groups = useDeviceStore((s) => s.groups);
  // Deleting a device and moving it between groups are configuration changes:
  // the server refuses them for a non-admin, so the controls are absent rather
  // than present-and-failing.
  const isAdmin = useAuthStore((s) => s.user?.is_admin ?? false);
  const fetchGroups = useDeviceStore((s) => s.fetchGroups);
  const updateDeviceGroup = useDeviceStore((s) => s.updateDeviceGroup);
  const moveDeviceOrganization = useDeviceStore((s) => s.moveDeviceOrganization);
  const organizations = useOrganizationStore((s) => s.organizations);
  const fetchOrganizations = useOrganizationStore((s) => s.fetchOrganizations);
  const restartAgent = useDeviceStore((s) => s.restartAgent);
  const setMaintenance = useDeviceStore((s) => s.setMaintenance);
  const hardware = useDeviceStore((s) => s.hardware);
  const fetchHardware = useDeviceStore((s) => s.fetchHardware);
  const refreshDevice = useDeviceStore((s) => s.refreshDevice);
  const upgradeAgent = useDeviceStore((s) => s.upgradeAgent);
  const manifests = useUpdateStore((s) => s.manifests);
  const fetchManifests = useUpdateStore((s) => s.fetchManifests);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmRestart, setConfirmRestart] = useState(false);
  const [isRestarting, setIsRestarting] = useState(false);
  const [isUpgrading, setIsUpgrading] = useState(false);
  const [confirmPowerAction, setConfirmPowerAction] = useState<PowerAction | null>(null);
  const [selectedGroupId, setSelectedGroupId] = useState('');
  const [selectedOrganizationId, setSelectedOrganizationId] = useState('');
  // Collapsed on open: the inventory is reference detail, so the host card
  // stays scannable until an operator asks for it.
  const [showHardware, setShowHardware] = useState(false);
  // Correlation jump target: the metrics panel hands up a unix-second window,
  // which the logs explorer consumes (as ISO) to pre-filter and fetch.
  const [logWindow, setLogWindow] = useState<{ from: string; to: string } | null>(null);

  useEffect(() => {
    if (id) {
      fireAndForget(fetchDevice(id));
      fireAndForget(fetchSessions(id));
    }
    fireAndForget(fetchGroups());
    fireAndForget(fetchOrganizations());
    fireAndForget(fetchManifests());
  }, [id, fetchDevice, fetchSessions, fetchGroups, fetchOrganizations, fetchManifests]);

  // Poll device data every 30s so agent_version and status stay in sync, and
  // re-read the session list on the same beat so a session that ended anywhere
  // — tab closed, agent restarted, relay torn down — leaves the card.
  // Uses refreshDevice (not fetchDevice) to preserve hardware/logs state.
  useVisibleInterval(() => {
    if (!id) return;
    fireAndForget(refreshDevice(id));
    fireAndForget(fetchSessions(id));
  }, DEVICE_DETAIL_POLL_MS);

  // Pull the hardware inventory once whenever the agent first appears online and
  // on each offline→online transition (a reboot refreshes it) — but never on a
  // steady-state poll. The agent reports fresh hardware as it registers, so the
  // row this reads is the one the reconnect just wrote.
  const prevStatusRef = useRef<string | null>(null);
  useEffect(() => {
    const status = device?.status;
    const previous = prevStatusRef.current;
    prevStatusRef.current = status ?? null;
    if (id && status === 'online' && previous !== 'online') {
      fireAndForget(fetchHardware(id));
    }
  }, [id, device?.status, fetchHardware]);

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
    const amtUuid = device?.amt?.uuid;
    if (!amtUuid) return;
    const ok = await sendPowerAction(amtUuid, action);
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
    fireAndForget(navigate('/devices'));
  };

  const handleToggleMaintenance = async (enabled: boolean, reason?: string): Promise<boolean> => {
    const ok = await setMaintenance(device.id, enabled, reason);
    if (ok) {
      addToast(enabled ? 'Device entered maintenance' : 'Device resumed — monitoring restored', 'success');
    } else {
      addToast(enabled ? 'Failed to enter maintenance' : 'Failed to resume device', 'error');
    }
    return ok;
  };

  const handleMoveOrganization = async () => {
    if (!selectedOrganizationId || selectedOrganizationId === device.organization_id) return;
    const ok = await moveDeviceOrganization(device.id, selectedOrganizationId);
    if (ok) {
      addToast('Device moved to new customer', 'success');
      setSelectedOrganizationId('');
    } else {
      addToast('Failed to move device', 'error');
    }
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
      fireAndForget(navigate(`/sessions/${result.token}`, { state: { relayUrl: result.relay_url, capabilities: device.capabilities } }));
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

  let restartButtonLabel = 'Restart Agent';
  if (isRestarting) restartButtonLabel = 'Restarting...';
  else if (confirmRestart) restartButtonLabel = `Confirm (${sessions.length} active)`;

  return (
    <div className="p-6 grid grid-cols-1 lg:grid-cols-2 gap-4 items-start">
      {/* Device Detail Card */}
      <div className="bg-gray-800 border border-gray-700 rounded-lg p-6 space-y-4">
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-3 flex-wrap">
            <h2 className="text-xl font-bold">{device.hostname}</h2>
            <StatusBadge status={device.status} />
            <AmtBadge amt={device.amt} />
            {device.maintenance_on && <MaintenanceBadge since={device.maintenance_since} />}
          </div>
          <div className="flex gap-2 flex-wrap justify-end">
            <button
              type="button"
              onClick={() => { fireAndForget(handleStartSession()); }}
              aria-label="Start Session"
              title="Start Session"
              className="px-2.5 py-1.5 bg-green-500 hover:bg-green-600 rounded text-xs font-medium inline-flex items-center"
            >
              <PlayIcon />
            </button>
            <button
              type="button"
              onClick={() => { fireAndForget(handleRestart()); }}
              disabled={device.status !== 'online' || isRestarting}
              aria-label={restartButtonLabel}
              title={restartButtonLabel}
              className={`px-2.5 py-1.5 rounded text-xs font-medium disabled:opacity-50 inline-flex items-center ${confirmRestart ? 'bg-yellow-500 ring-2 ring-yellow-300' : 'bg-yellow-600 hover:bg-yellow-700'}`}
            >
              {isRestarting ? <SpinnerIcon /> : <RestartIcon />}
            </button>
            {latestManifest && !isUpToDate && (
              <button
                type="button"
                onClick={() => { fireAndForget(handleUpgrade()); }}
                disabled={isUpgrading || device.status !== 'online'}
                className="px-3 py-1.5 bg-green-600 hover:bg-green-700 rounded text-xs font-medium disabled:opacity-50"
              >
                {isUpgrading ? 'Upgrading...' : `Upgrade to v${latestManifest.version}`}
              </button>
            )}
            {isUpToDate && (
              <span
                role="img"
                aria-label="Up to date"
                title="Up to date"
                className="px-2.5 py-1.5 bg-gray-700 text-green-400 rounded text-xs font-medium inline-flex items-center"
              >
                <CheckIcon />
              </span>
            )}
            {isAdmin && confirmDelete && (
              <span role="alert" className="px-2 py-1.5 text-xs text-red-300">
                Irreversible: permanently erases all of this device&apos;s telemetry and deprovisions its agent.
              </span>
            )}
            {isAdmin && (
              <button
                type="button"
                onClick={() => { fireAndForget(handleDelete()); }}
                aria-label={confirmDelete ? 'Confirm Delete' : 'Delete Device'}
                title={confirmDelete ? 'Confirm Delete' : 'Delete Device'}
                className={`px-2.5 py-1.5 rounded text-xs font-medium inline-flex items-center ${confirmDelete ? 'bg-red-500 ring-2 ring-red-300' : 'bg-red-600 hover:bg-red-700'}`}
              >
                <TrashIcon />
              </button>
            )}
          </div>
        </div>

        <dl className="grid grid-cols-2 gap-3 text-sm">
          <div>
            <dt className="text-gray-400">OS</dt>
            <dd>{device.os_display || device.os}</dd>
          </div>
          <div>
            <dt className="text-gray-400">Group ID</dt>
            <dd className="font-mono text-xs">{isUnassignedGroup(device.group_id) ? 'N/A' : device.group_id}</dd>
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

        <MaintenancePanel device={device} onToggle={handleToggleMaintenance} />

        {isAdmin && organizations.length > 1 && (
          <div>
            <h3 className="text-sm font-semibold text-gray-300 mb-2">Move to Customer</h3>
            <div className="flex gap-2">
              <select
                aria-label="Move to customer"
                value={selectedOrganizationId}
                onChange={(e) => setSelectedOrganizationId(e.target.value)}
                className="bg-gray-900 border border-gray-600 rounded px-3 py-1.5 text-sm flex-1"
              >
                <option value="">Select customer...</option>
                {organizations.filter((o) => o.id !== device.organization_id).map((o) => (
                  <option key={o.id} value={o.id}>{o.name}</option>
                ))}
              </select>
              <button
                type="button"
                onClick={() => { fireAndForget(handleMoveOrganization()); }}
                disabled={!selectedOrganizationId}
                className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 rounded text-sm disabled:opacity-50"
              >
                Move
              </button>
            </div>
          </div>
        )}

        {isAdmin && groups.length > 1 && (
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
                onClick={() => { fireAndForget(handleMoveGroup()); }}
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

        <AmtSection
          amt={device.amt}
          confirmPowerAction={confirmPowerAction}
          onPowerAction={(action) => { fireAndForget(handlePowerAction(action)); }}
        />

        <div>
          <div className="flex items-center justify-between mb-2">
            <button
              type="button"
              onClick={() => setShowHardware(!showHardware)}
              aria-expanded={showHardware}
              className="text-sm font-semibold text-gray-300 flex items-center gap-2"
            >
              <span className={`text-xs transition-transform ${showHardware ? 'rotate-90' : ''}`} aria-hidden="true">&#9654;</span>
              {' '}Hardware
            </button>
          </div>
          {showHardware && !hardware && (
            <p className="text-xs text-gray-500">Hardware inventory not reported yet.</p>
          )}
          {showHardware && hardware && (
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
                {hardware.amt_available && (
                  <div>
                    <dt className="text-gray-400">Intel AMT</dt>
                    <dd>
                      {hardware.amt_model || 'Supported'}
                      {hardware.amt_firmware && ` — firmware ${hardware.amt_firmware}`}
                      {!hardware.amt_firmware && hardware.amt_version && ` — ME ${hardware.amt_version}`}
                    </dd>
                  </div>
                )}
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

      {/* Agent Logs Card (the agent's own files; browsable, no correlation jump) */}
      <div className="bg-gray-800 border border-gray-700 rounded-lg p-6">
        <DeviceLogs deviceId={device.id} />
      </div>

      {/* System Logs (the platform host log, journald on Linux; drill target), full width */}
      <div className="lg:col-span-2 bg-gray-800 border border-gray-700 rounded-lg p-6">
        <SystemLogs deviceId={device.id} focusWindow={logWindow} />
      </div>

      {/* Discovered footprint (ports / services / DB engines / containers / packages), full width */}
      <div className="lg:col-span-2 bg-gray-800 border border-gray-700 rounded-lg p-6">
        <DeviceInventory
          deviceId={device.id}
          maintenanceSince={device.maintenance_on ? device.maintenance_since : undefined}
        />
      </div>

      {/* Telemetry (metrics timelines + anomaly correlation), full width */}
      <div className="lg:col-span-2 bg-gray-800 border border-gray-700 rounded-lg p-6">
        <h3 className="text-sm font-semibold text-gray-300 mb-3">Telemetry</h3>
        <DeviceMetrics
          deviceId={device.id}
          anomalyRate={device.anomaly_rate}
          maintenanceSince={device.maintenance_on ? device.maintenance_since : undefined}
          onViewLogs={(fromSec, toSec) => {
            setLogWindow({
              from: new Date(fromSec * 1000).toISOString(),
              to: new Date(toSec * 1000).toISOString(),
            });
          }}
        />
      </div>
    </div>
  );
}
