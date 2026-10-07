import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction } from '../../../state/api-action';
import { useToastStore } from '../../../lib/feedback/toast-store';
import type { components } from '../../../types/api';
import { fireAndForget } from '../../../lib/fire-and-forget';
import { selectedOrganizationQuery, useOrganizationStore } from '../../organizations';
import { NOT_ASSIGNED_SITE_ID } from '../device-drag';

type Device = components['schemas']['Device'];
type Site = components['schemas']['Site'];
type DeviceHardware = components['schemas']['DeviceHardware'];
type DeviceLogsResponse = components['schemas']['DeviceLogsResponse'];
type MetricRangeResponse = components['schemas']['MetricRangeResponse'];
type DeviceSummary = components['schemas']['DeviceSummary'];
type PowerAction = components['schemas']['AMTPowerRequest']['action'];

/** The log pane a fetch targets: the agent's own files or the platform host log. */
export type LogPaneSource = 'agent' | 'system';

/** Filters accepted by a log fetch; `unit` applies to the system pane only. */
export interface LogFetchParams {
  level?: string;
  from?: string;
  to?: string;
  search?: string;
  unit?: string;
  offset?: number;
  limit?: number;
}

/** Downsampled-window request for the device metrics timelines. */
export interface MetricsParams {
  from: string;
  to: string;
  dims?: string[];
  maxPoints?: number;
  band?: 'none' | 'avg_of_60s';
}

interface DeviceState {
  devices: Device[];
  sites: Site[];
  selectedSiteId: string | null;
  selectedDevice: Device | null;
  hardware: DeviceHardware | null;
  /** Per-pane log responses, keyed by source so the two panes stay independent. */
  logs: Record<LogPaneSource, DeviceLogsResponse | null>;
  /** The device each pane's payload belongs to, which lets a re-opened page reuse its logs. */
  logsDeviceId: Record<LogPaneSource, string | null>;
  /** Per-pane in-flight flags, keyed by source. */
  logsLoading: Record<LogPaneSource, boolean>;
  metrics: MetricRangeResponse | null;
  metricsLoading: boolean;
  /** Fixed-size fleet rollup behind the dashboard tiles. Null until first load. */
  summary: DeviceSummary | null;
  isLoading: boolean;
  error: string | null;
  fetchSites: () => Promise<void>;
  fetchDevices: (siteId?: string) => Promise<void>;
  /** Move a device to another customer. Returns whether the move landed. */
  moveDeviceOrganization: (id: string, organizationId: string) => Promise<boolean>;
  fetchDevice: (id: string) => Promise<void>;
  refreshDevice: (id: string) => Promise<void>;
  selectSite: (id: string | null) => void;
  createSite: (name: string) => Promise<void>;
  deleteSite: (id: string) => Promise<void>;
  deleteDevice: (id: string) => Promise<void>;
  updateDeviceSite: (id: string, siteId: string) => Promise<boolean>;
  restartAgent: (id: string) => Promise<boolean>;
  /** Sends a power command over Intel AMT, addressed by the AMT uuid the device payload carries. */
  sendPowerAction: (amtUuid: string, action: PowerAction) => Promise<boolean>;
  fetchHardware: (id: string) => Promise<void>;
  fetchLogs: (source: LogPaneSource, id: string, params?: LogFetchParams) => Promise<void>;
  fetchMetrics: (id: string, params: MetricsParams) => Promise<void>;
  upgradeAgent: (deviceId: string, version: string, os: string, arch: string) => Promise<boolean>;
  setMaintenance: (id: string, enabled: boolean, reason?: string) => Promise<boolean>;
  fetchSummary: () => Promise<void>;
}

async function retryHardwareFetch(set: (partial: Partial<DeviceState>) => void, id: string) {
  try {
    const retry = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}/hardware', { params: { path: { id } } }), false,
    );
    if (retry.ok) set({ hardware: retry.data });
  } catch (err) {
    useToastStore.getState().addToast(
      `Failed to refresh hardware: ${err instanceof Error ? err.message : String(err)}`,
      'error',
    );
  }
}

/** The in-flight log-pull chain per device; a second concurrent pull gets a 409. */
const logFetchQueue = new Map<string, Promise<void>>();

async function queueLogFetch(id: string, run: () => Promise<void>): Promise<void> {
  const turn = (logFetchQueue.get(id) ?? Promise.resolve()).then(run);
  // A failed pull still lets the next one run.
  logFetchQueue.set(id, turn.catch(() => undefined));
  try {
    await turn;
  } finally {
    if (logFetchQueue.get(id) === turn) logFetchQueue.delete(id);
  }
}

const logErrorMessages: Record<number, string> = {
  403: 'Viewing device logs requires administrator access.',
  404: 'Logs unavailable — device offline or not found.',
  409: 'A log request is already in progress for this device.',
  504: 'The device did not return logs in time.',
};

async function pullLogs(
  set: (partial: (state: DeviceState) => Partial<DeviceState>) => void,
  source: LogPaneSource,
  id: string,
  params?: LogFetchParams,
): Promise<void> {
  const query: Record<string, string | number> = { source: source === 'system' ? 'host' : 'self' };
  if (params?.level) query.level = params.level;
  if (params?.from) query.from = params.from;
  if (params?.to) query.to = params.to;
  if (params?.search) query.search = params.search;
  if (params?.unit) query.unit = params.unit;
  if (params?.offset !== undefined) query.offset = params.offset;
  if (params?.limit !== undefined) query.limit = params.limit;

  const { data, response } = await api.GET('/api/v1/devices/{id}/logs', {
    params: { path: { id }, query },
  });

  if (response.status === 200 && data) {
    set((s) => ({
      logs: { ...s.logs, [source]: data },
      logsDeviceId: { ...s.logsDeviceId, [source]: id },
      logsLoading: { ...s.logsLoading, [source]: false },
    }));
    return;
  }

  useToastStore.getState().addToast(logErrorMessages[response.status] ?? 'Failed to fetch logs.', 'error');
  set((s) => ({ logsLoading: { ...s.logsLoading, [source]: false } }));
}

export const useDeviceStore = create<DeviceState>((set, get) => ({
  devices: [],
  sites: [],
  selectedSiteId: null,
  selectedDevice: null,
  hardware: null,
  logs: { agent: null, system: null },
  logsDeviceId: { agent: null, system: null },
  logsLoading: { agent: false, system: false },
  metrics: null,
  metricsLoading: false,
  summary: null,
  isLoading: false,
  error: null,

  fetchSites: async () => {
    // The sidebar lists only the picked customer's sites.
    const organizationId = selectedOrganizationQuery();
    const res = await apiAction(set, () =>
      api.GET('/api/v1/sites', {
        params: { query: organizationId ? { organization_id: organizationId } : {} },
      }),
    );
    if (res.ok) set({ sites: res.data });
  },

  fetchDevices: async (siteId?) => {
    // The picked customer scopes the fleet and the site, or no site at all, narrows within it.
    let site = {};
    if (siteId === NOT_ASSIGNED_SITE_ID) site = { without_site: true };
    else if (siteId) site = { site_id: siteId };
    const query = {
      ...site,
      ...(selectedOrganizationQuery() ? { organization_id: selectedOrganizationQuery() } : {}),
    };
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices', { params: { query } }),
    );
    if (res.ok) set({ devices: res.data });
  },

  fetchDevice: async (id) => {
    // A log pane already holding this device's logs keeps them, so a re-opened page issues no pull.
    set((s) => ({
      selectedDevice: null,
      hardware: null,
      logs: {
        agent: s.logsDeviceId.agent === id ? s.logs.agent : null,
        system: s.logsDeviceId.system === id ? s.logs.system : null,
      },
      logsDeviceId: {
        agent: s.logsDeviceId.agent === id ? id : null,
        system: s.logsDeviceId.system === id ? id : null,
      },
      metrics: null,
    }));
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}', { params: { path: { id } } }),
    );
    if (res.ok) set({ selectedDevice: res.data });
  },

  refreshDevice: async (id) => {
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}', { params: { path: { id } } }), false,
    );
    if (res.ok) set({ selectedDevice: res.data });
  },

  selectSite: (id) => {
    set({ selectedSiteId: id });
    fireAndForget(get().fetchDevices(id ?? undefined));
  },

  createSite: async (name) => {
    // A new site belongs to the picked customer; with none picked the server uses the tenant's own.
    const organizationId = selectedOrganizationQuery();
    const res = await apiAction(set, () =>
      api.POST('/api/v1/sites', {
        body: organizationId ? { name, organization_id: organizationId } : { name },
      }), false,
    );
    if (res.ok) set((state) => ({ sites: [...state.sites, res.data] }));
  },

  deleteSite: async (id) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/sites/{id}', { params: { path: { id } } }), false,
    );
    if (res.ok) {
      set((state) => ({
        sites: state.sites.filter((g) => g.id !== id),
        selectedSiteId: state.selectedSiteId === id ? null : state.selectedSiteId,
      }));
    }
  },

  deleteDevice: async (id) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/devices/{id}', { params: { path: { id } } }), false,
    );
    if (res.ok) {
      set((state) => ({
        devices: state.devices.filter((d) => d.id !== id),
      }));
    }
  },

  moveDeviceOrganization: async (id, organizationId) => {
    const res = await apiAction(set, () =>
      api.PUT('/api/v1/devices/{id}/organization', {
        params: { path: { id } },
        body: { organization_id: organizationId },
      }), false,
    );
    if (res.ok) {
      set((state) => ({
        selectedDevice: state.selectedDevice?.id === id ? res.data : state.selectedDevice,
        devices: state.devices.map((d) => (d.id === id ? res.data : d)),
      }));
    }
    return res.ok;
  },

  updateDeviceSite: async (id, siteId) => {
    const res = await apiAction(set, () =>
      api.PATCH('/api/v1/devices/{id}', {
        params: { path: { id } },
        body: { site_id: siteId },
      }), false,
    );
    if (res.ok) {
      set((state) => ({
        selectedDevice: state.selectedDevice?.id === id ? res.data : state.selectedDevice,
        devices: state.devices.map((d) => (d.id === id ? res.data : d)),
      }));
    }
    return res.ok;
  },

  restartAgent: async (id) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/devices/{id}/restart', {
        params: { path: { id } },
        body: { reason: 'restart requested from web UI' },
      }), false,
    );
    return res.ok;
  },

  sendPowerAction: async (amtUuid, action) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/amt/devices/{uuid}/power', {
        params: { path: { uuid: amtUuid } },
        body: { action },
      }), false,
    );
    return res.ok;
  },

  fetchHardware: async (id) => {
    // A failed refresh keeps the last known inventory; fetchDevice blanks it on a device switch.
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}/hardware', {
        params: { path: { id } },
      }), false,
    );
    if (res.ok) {
      set({ hardware: res.data });
    } else {
      // A failed read retries once after 2s for the agent's response.
      setTimeout(() => { fireAndForget(retryHardwareFetch(set, id)); }, 2000);
    }
  },

  fetchLogs: async (source, id, params) => {
    // The flag is raised before queueing so a waiting pane already shows as loading.
    set((s) => ({ logsLoading: { ...s.logsLoading, [source]: true } }));
    await queueLogFetch(id, () => pullLogs(set, source, id, params));
  },

  fetchMetrics: async (id, params) => {
    set({ metricsLoading: true });
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}/metrics', {
        params: {
          path: { id },
          query: {
            from: params.from,
            to: params.to,
            ...(params.dims && params.dims.length > 0 ? { dims: params.dims } : {}),
            ...(params.maxPoints != null ? { max_points: params.maxPoints } : {}),
            ...(params.band ? { band: params.band } : {}),
          },
        },
      }), false,
    );
    if (res.ok) set({ metrics: res.data, metricsLoading: false });
    else set({ metricsLoading: false });
  },

  upgradeAgent: async (deviceId, version, os, arch) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/updates/push', {
        body: { version, os, arch, device_ids: [deviceId] },
      }), false,
    );
    return res.ok;
  },

  setMaintenance: async (id, enabled, reason) => {
    // The server persists maintenance and reconciles it to the agent, so this works offline.
    const res = await apiAction(set, () =>
      api.POST('/api/v1/devices/{id}/maintenance', {
        params: { path: { id } },
        body: { enabled, ...(reason ? { reason } : {}) },
      }), false,
    );
    if (res.ok) {
      set((state) => ({
        selectedDevice: state.selectedDevice?.id === id ? res.data : state.selectedDevice,
        devices: state.devices.map((d) => (d.id === id ? res.data : d)),
      }));
    }
    return res.ok;
  },

  fetchSummary: async () => {
    // The rollup narrows to the same customer as the device list.
    const organizationId = selectedOrganizationQuery();
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/summary', {
        params: { query: organizationId ? { organization_id: organizationId } : {} },
      }), false,
    );
    if (res.ok) set({ summary: res.data });
  },
}));

// A picked site belongs to one customer, so choosing another customer drops it.
useOrganizationStore.subscribe((state, previous) => {
  if (state.selectedOrganizationId === previous.selectedOrganizationId) return;
  useDeviceStore.setState({ selectedSiteId: null });
});
