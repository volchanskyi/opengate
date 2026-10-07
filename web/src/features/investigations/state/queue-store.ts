import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction, progressAdapter } from '../../../state/api-action';
import { selectedOrganizationQuery, useOrganizationStore } from '../../organizations';
import type { components } from '../../../types/api';

type Incident = components['schemas']['Incident'];
type Status = components['schemas']['IncidentStatus'];
type Severity = components['schemas']['IncidentSeverity'];

export interface QueueFilters {
  /** One status at a time; the request carries it as a one-item list. */
  status: Status;
  severity: readonly Severity[];
  ruleId: string;
  deviceId: string;
}

/** OPEN_STATUSES lists the statuses the queue opens on, those still needing action. */
export const OPEN_STATUSES: readonly Status[] = ['new', 'acknowledged', 'investigating'];

export const DEFAULT_QUEUE_FILTERS: QueueFilters = {
  status: 'new',
  severity: [],
  ruleId: '',
  deviceId: '',
};

interface QueueState {
  items: Incident[];
  nextCursor: string | null;
  loading: boolean;
  loaded: boolean;
  // A background re-read starts from the top, so it skips a queue already paged past page one.
  pagedOn: boolean;
  error: string | null;
  filters: QueueFilters;
  byDevice: Map<string, Incident[]>;
  deviceErrors: Map<string, string>;

  setFilters: (patch: Partial<QueueFilters>) => void;
  fetchQueue: () => Promise<void>;
  fetchMore: () => Promise<void>;
  fetchDeviceIncidents: (deviceId: string) => Promise<void>;
}

function narrowedQuery(filters: QueueFilters, cursor: string | null) {
  const organizationId = selectedOrganizationQuery();
  return {
    ...(organizationId ? { organization_id: organizationId } : {}),
    status: [filters.status],
    ...(filters.severity.length > 0 ? { severity: [...filters.severity] } : {}),
    ...(filters.ruleId ? { rule_id: filters.ruleId } : {}),
    ...(filters.deviceId ? { device_id: filters.deviceId } : {}),
    ...(cursor ? { cursor } : {}),
  };
}

export const useQueueStore = create<QueueState>((set, get) => {
  // A failed read keeps the rows already on screen.
  const queueProgress = progressAdapter(
    (error) => { set({ error }); },
    (loading) => { set({ loading }); },
  );

  const deviceProgress = (deviceId: string) => progressAdapter((error) => {
    set((s) => {
      const deviceErrors = new Map(s.deviceErrors);
      if (error === null) deviceErrors.delete(deviceId);
      else deviceErrors.set(deviceId, error);
      return { deviceErrors };
    });
  });

  const readPage = (cursor: string | null) =>
    apiAction(queueProgress, () => api.GET('/api/v1/investigations', {
      params: { query: narrowedQuery(get().filters, cursor) },
    }));

  return {
    items: [],
    nextCursor: null,
    loading: false,
    loaded: false,
    pagedOn: false,
    error: null,
    filters: DEFAULT_QUEUE_FILTERS,
    byDevice: new Map(),
    deviceErrors: new Map(),

    setFilters: (patch) => {
      set((s) => ({ filters: { ...s.filters, ...patch } }));
    },

    fetchQueue: async () => {
      const res = await readPage(null);
      if (!res.ok) return;
      set({ items: res.data.items, nextCursor: res.data.next_cursor ?? null, loaded: true, pagedOn: false });
    },

    fetchMore: async () => {
      const { nextCursor, loading } = get();
      if (nextCursor === null || loading) return;

      const res = await readPage(nextCursor);
      if (!res.ok) return;
      set((s) => ({
        items: [...s.items, ...res.data.items],
        nextCursor: res.data.next_cursor ?? null,
        pagedOn: true,
      }));
    },

    fetchDeviceIncidents: async (deviceId) => {
      const res = await apiAction(
        deviceProgress(deviceId),
        () => api.GET('/api/v1/devices/{id}/incidents', {
          params: { path: { id: deviceId }, query: { status: [...OPEN_STATUSES] } },
        }),
        false,
      );
      if (!res.ok) return;
      set((s) => ({ byDevice: new Map(s.byDevice).set(deviceId, res.data.items) }));
    },
  };
});

// A picked host belongs to one customer, so choosing another customer drops it.
useOrganizationStore.subscribe((state, previous) => {
  if (state.selectedOrganizationId === previous.selectedOrganizationId) return;
  useQueueStore.setState((s) => ({ filters: { ...s.filters, deviceId: '' } }));
});
