import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction, progressAdapter } from '../../../state/api-action';
import type { HostOption } from '../../../components/HostSelect';

/** One site a pick-list offers. */
export interface SiteOption {
  readonly id: string;
  readonly name: string;
}

/** The key the whole tenant's hosts are held under, beside each customer's. */
export const WHOLE_TENANT = '';

/** Hosts and sites for pick-lists and names, held apart from the Devices page's own list. */
interface PickListState {
  /** By customer id, or by WHOLE_TENANT for every host the caller may see. */
  hosts: ReadonlyMap<string, readonly HostOption[]>;
  sites: ReadonlyMap<string, readonly SiteOption[]>;
  error: string | null;

  fetchHosts: (customerId: string | null) => Promise<void>;
  fetchSites: (customerId: string) => Promise<void>;
}

/** By name ignoring case, then by id, so two hosts of one name keep a stable order. */
function byName(a: { name: string; id: string }, b: { name: string; id: string }): number {
  return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.id.localeCompare(b.id);
}

export const usePickListStore = create<PickListState>((set) => {
  const progress = progressAdapter((error) => { set({ error }); });

  return {
    hosts: new Map(),
    sites: new Map(),
    error: null,

    fetchHosts: async (customerId) => {
      const query = customerId === null ? {} : { organization_id: customerId };
      const res = await apiAction(progress, () => api.GET('/api/v1/devices', { params: { query } }), false);
      if (!res.ok) return;
      const hosts = res.data
        .map((d) => ({ id: d.id, name: d.hostname, online: d.status === 'online' }))
        .sort(byName);
      set((s) => ({ hosts: new Map(s.hosts).set(customerId ?? WHOLE_TENANT, hosts) }));
    },

    fetchSites: async (customerId) => {
      const res = await apiAction(progress, () =>
        api.GET('/api/v1/sites', { params: { query: { organization_id: customerId } } }), false);
      if (!res.ok) return;
      const sites = res.data.map((s) => ({ id: s.id, name: s.name })).sort(byName);
      set((s) => ({ sites: new Map(s.sites).set(customerId, sites) }));
    },
  };
});
