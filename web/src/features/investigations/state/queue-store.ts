import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction, progressAdapter } from '../../../state/api-action';
import { selectedOrganizationQuery } from '../../organizations';
import type { components } from '../../../types/api';

type Incident = components['schemas']['Incident'];
type Status = components['schemas']['IncidentStatus'];
type Severity = components['schemas']['IncidentSeverity'];

/** How the queue is narrowed. Empty everywhere means "the whole tenant". */
export interface QueueFilters {
  status: readonly Status[];
  severity: readonly Severity[];
  ruleId: string;
  deviceId: string;
}

/**
 * The queue opens on what is still somebody's problem. A resolved room is
 * history, and starting on the whole history is how a triage queue reads as
 * hundreds of rows nobody has to act on.
 */
export const OPEN_STATUSES: readonly Status[] = ['new', 'acknowledged', 'investigating'];

export const DEFAULT_QUEUE_FILTERS: QueueFilters = {
  status: OPEN_STATUSES,
  severity: [],
  ruleId: '',
  deviceId: '',
};

interface QueueState {
  items: Incident[];
  /** Where the next page starts, or null at the end of the queue. */
  nextCursor: string | null;
  loading: boolean;
  /** Whether the queue has been read, so an empty queue is an answer. */
  loaded: boolean;
  /**
   * Whether somebody has read past the first page. A background re-read starts
   * from the top, so it would throw away the pages they walked to.
   */
  pagedOn: boolean;
  error: string | null;
  filters: QueueFilters;
  /** Open incidents per device id. An absent key means "not yet read". */
  byDevice: Map<string, Incident[]>;
  deviceErrors: Map<string, string>;

  setFilters: (patch: Partial<QueueFilters>) => void;
  /** Read the queue from the top. Any cursor in hand is dropped. */
  fetchQueue: () => Promise<void>;
  /** Read on from where the last page ended. */
  fetchMore: () => Promise<void>;
  fetchDeviceIncidents: (deviceId: string) => Promise<void>;
}

function narrowedQuery(filters: QueueFilters, cursor: string | null) {
  const organizationId = selectedOrganizationQuery();
  return {
    ...(organizationId ? { organization_id: organizationId } : {}),
    ...(filters.status.length > 0 ? { status: [...filters.status] } : {}),
    ...(filters.severity.length > 0 ? { severity: [...filters.severity] } : {}),
    ...(filters.ruleId ? { rule_id: filters.ruleId } : {}),
    ...(filters.deviceId ? { device_id: filters.deviceId } : {}),
    ...(cursor ? { cursor } : {}),
  };
}

export const useQueueStore = create<QueueState>((set, get) => {
  // A failed read keeps the rows already on screen: a poll that fails mid-shift
  // must not empty the queue somebody is working.
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
