import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction } from '../../../state/api-action';
import { useToastStore } from '../../../lib/feedback/toast-store';
import type { components } from '../../../types/api';
import { fireAndForget } from '../../../lib/fire-and-forget';
import { selectedOrganizationQuery } from '../../organizations';

type Device = components['schemas']['Device'];
type Site = components['schemas']['Site'];
type DeviceHardware = components['schemas']['DeviceHardware'];
type DeviceLogsResponse = components['schemas']['DeviceLogsResponse'];
type MetricRangeResponse = components['schemas']['MetricRangeResponse'];
type CorrelateResponse = components['schemas']['CorrelateResponse'];
type DeviceSummary = components['schemas']['DeviceSummary'];
type PowerAction = components['schemas']['AMTPowerRequest']['action'];

/**
 * Which log pane a fetch targets: `agent` reads the agent's own rotated files;
 * `system` reads the platform host log (journald on Linux). The two panes hold
 * independent state so one never clobbers the other.
 */
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

/** Focus/baseline window for the on-demand correlation drill-down. */
export interface CorrelateParams {
  focusStart: string;
  focusEnd: string;
  baselineStart?: string;
  baselineEnd?: string;
  topN?: number;
}

interface DeviceState {
  devices: Device[];
  sites: Site[];
  selectedSiteId: string | null;
  selectedDevice: Device | null;
  hardware: DeviceHardware | null;
  /** Per-pane log responses, keyed by source so the two panes stay independent. */
  logs: Record<LogPaneSource, DeviceLogsResponse | null>;
  /** Which device each pane's payload belongs to — the cache key that lets a
   *  re-opened device page render its logs without pulling them again. */
  logsDeviceId: Record<LogPaneSource, string | null>;
  /** Per-pane in-flight flags, keyed by source. */
  logsLoading: Record<LogPaneSource, boolean>;
  metrics: MetricRangeResponse | null;
  metricsLoading: boolean;
  correlation: CorrelateResponse | null;
  correlationLoading: boolean;
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
  /** Sends an out-of-band power command over the device's Intel AMT connection.
   *  Addressed by the AMT uuid the device payload carries, not the device id. */
  sendPowerAction: (amtUuid: string, action: PowerAction) => Promise<boolean>;
  fetchHardware: (id: string) => Promise<void>;
  fetchLogs: (source: LogPaneSource, id: string, params?: LogFetchParams) => Promise<void>;
  fetchMetrics: (id: string, params: MetricsParams) => Promise<void>;
  correlate: (id: string, params: CorrelateParams) => Promise<void>;
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

/**
 * Tail of the in-flight log-pull chain per device. The server brokers exactly
 * one raw-log request per agent at a time and answers a second one with 409, so
 * the Agent Logs and System Logs panes take turns rather than racing.
 */
const logFetchQueue = new Map<string, Promise<void>>();

async function queueLogFetch(id: string, run: () => Promise<void>): Promise<void> {
  const turn = (logFetchQueue.get(id) ?? Promise.resolve()).then(run);
  // Failures must not poison the queue for the next pull.
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
  // The agent pane reads the agent's own files ("self"); the system pane reads
  // the platform host log ("host"). The unit filter applies to the host source.
  const query: Record<string, string | number> = { source: source === 'system' ? 'host' : 'self' };
  if (params?.level) query.level = params.level;
  if (params?.from) query.from = params.from;
  if (params?.to) query.to = params.to;
  if (params?.search) query.search = params.search;
  if (params?.unit) query.unit = params.unit;
  if (params?.offset !== undefined) query.offset = params.offset;
  if (params?.limit !== undefined) query.limit = params.limit;

  // The server brokers the pull straight from the agent and blocks until it
  // responds, so a single request returns the logs (or a bounded failure).
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
  correlation: null,
  correlationLoading: false,
  summary: null,
  isLoading: false,
  error: null,

  fetchSites: async () => {
    // The sidebar describes the picked customer's locations, so a technician
    // looking at Contoso never sees Fabrikam's offices in the filter.
    const organizationId = selectedOrganizationQuery();
    const res = await apiAction(set, () =>
      api.GET('/api/v1/sites', {
        params: { query: organizationId ? { organization_id: organizationId } : {} },
      }),
    );
    if (res.ok) set({ sites: res.data });
  },

  fetchDevices: async (siteId?) => {
    // Both narrowings travel together: the picked customer scopes the fleet, the
    // site narrows within it, and an absent value simply does not narrow.
    const query = {
      ...(siteId ? { site_id: siteId } : {}),
      ...(selectedOrganizationQuery() ? { organization_id: selectedOrganizationQuery() } : {}),
    };
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices', { params: { query } }),
    );
    if (res.ok) set({ devices: res.data });
  },

  fetchDevice: async (id) => {
    // Reset per-device fields so stale data from a previously viewed device
    // does not leak into this one while we wait for the fetch to complete.
    // Log panes are the exception: a pane already holding this device's logs
    // keeps them, so re-opening a device page renders from cache and issues no
    // pull (the agent broker serves one log request at a time).
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
      correlation: null,
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
    // A new site belongs to the customer being looked at; with none picked the
    // server files it under the tenant's own.
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
      // Keep both views in step: the detail pane (when it is this device) and
      // the card the user dragged out of the list.
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
    // The inventory is replaced only by a successful pull. A device switch is
    // already blanked by fetchDevice, so holding the last known values through
    // a failed refresh keeps the card useful — its Last Updated stamp shows how
    // old they are.
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}/hardware', {
        params: { path: { id } },
      }), false,
    );
    if (res.ok) {
      set({ hardware: res.data });
    } else {
      // 202 (report requested) or 404 — retry once after 2s in case the agent responds
      setTimeout(() => { fireAndForget(retryHardwareFetch(set, id)); }, 2000);
    }
  },

  fetchLogs: async (source, id, params) => {
    // The flag is raised before the queue so both panes show "Fetching…" the
    // moment they ask, even while one waits its turn behind the other.
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

  correlate: async (id, params) => {
    set({ correlationLoading: true });
    const res = await apiAction(set, () =>
      api.POST('/api/v1/devices/{id}/correlate', {
        params: { path: { id } },
        body: {
          focus_start: params.focusStart,
          focus_end: params.focusEnd,
          ...(params.baselineStart ? { baseline_start: params.baselineStart } : {}),
          ...(params.baselineEnd ? { baseline_end: params.baselineEnd } : {}),
          ...(params.topN != null ? { top_n: params.topN } : {}),
        },
      }), false,
    );
    if (res.ok) set({ correlation: res.data, correlationLoading: false });
    else set({ correlationLoading: false });
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
    // Maintenance is a desired state, not a live command — the server persists
    // it and reconciles to the agent over the control channel, so this succeeds
    // even when the device is offline. An empty reason is omitted so an exit
    // never records a stray note.
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
    // One fixed-size request drives every dashboard tile, so the dashboard
    // never downloads the device list to count it.
    // The rollup narrows to the same customer as the list, so the tiles and the
    // fleet below them always describe one set.
    const organizationId = selectedOrganizationQuery();
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/summary', {
        params: { query: organizationId ? { organization_id: organizationId } : {} },
      }), false,
    );
    if (res.ok) set({ summary: res.data });
  },
}));
