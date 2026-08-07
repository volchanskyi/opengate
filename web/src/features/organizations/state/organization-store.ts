import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction } from '../../../state/api-action';
import type { components } from '../../../types/api';

type Organization = components['schemas']['Organization'];

/**
 * Where the picked customer is remembered. A technician works one customer at a
 * time for stretches, so the choice survives a reload rather than snapping back
 * to the whole tenant on every visit.
 */
export const SELECTED_ORGANIZATION_KEY = 'selectedOrganizationId';

interface OrganizationState {
  organizations: Organization[];
  /** The customer the fleet views narrow to, or null for the whole tenant. */
  selectedOrganizationId: string | null;
  isLoading: boolean;
  error: string | null;

  fetchOrganizations: (includeArchived?: boolean) => Promise<void>;
  selectOrganization: (id: string | null) => void;
  hydrateSelection: () => void;
  createOrganization: (name: string) => Promise<boolean>;
  renameOrganization: (id: string, name: string) => Promise<boolean>;
  setOrganizationArchived: (id: string, archived: boolean) => Promise<boolean>;
  deleteOrganization: (id: string) => Promise<boolean>;
}

function rememberSelection(id: string | null): void {
  if (id === null) {
    localStorage.removeItem(SELECTED_ORGANIZATION_KEY);
    return;
  }
  localStorage.setItem(SELECTED_ORGANIZATION_KEY, id);
}

export const useOrganizationStore = create<OrganizationState>((set, get) => ({
  organizations: [],
  selectedOrganizationId: null,
  isLoading: false,
  error: null,

  fetchOrganizations: async (includeArchived = false) => {
    const query = includeArchived ? { include_archived: true } : {};
    const res = await apiAction(set, () =>
      api.GET('/api/v1/organizations', { params: { query } }),
    );
    if (!res.ok) return;

    // A customer that has been deleted or archived away must not stay selected,
    // or the fleet views would narrow to something the tenant no longer has and
    // read as an empty fleet.
    const selected = get().selectedOrganizationId;
    const stillThere = selected !== null && res.data.some((o) => o.id === selected);
    if (selected !== null && !stillThere) {
      rememberSelection(null);
    }
    set({ organizations: res.data, selectedOrganizationId: stillThere ? selected : null });
  },

  selectOrganization: (id) => {
    rememberSelection(id);
    set({ selectedOrganizationId: id });
  },

  hydrateSelection: () => {
    set({ selectedOrganizationId: localStorage.getItem(SELECTED_ORGANIZATION_KEY) });
  },

  createOrganization: async (name) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/organizations', { body: { name } }), false,
    );
    if (res.ok) set((state) => ({ organizations: [...state.organizations, res.data] }));
    return res.ok;
  },

  renameOrganization: async (id, name) => {
    const res = await apiAction(set, () =>
      api.PATCH('/api/v1/organizations/{id}', {
        params: { path: { id } },
        body: { name },
      }), false,
    );
    if (res.ok) {
      set((state) => ({
        organizations: state.organizations.map((o) => (o.id === id ? res.data : o)),
      }));
    }
    return res.ok;
  },

  setOrganizationArchived: async (id, archived) => {
    const res = await apiAction(set, () =>
      api.PATCH('/api/v1/organizations/{id}', {
        params: { path: { id } },
        body: { archived },
      }), false,
    );
    if (res.ok) {
      // An archived customer leaves the working set the picker offers; a
      // restored one takes its updated row back.
      set((state) => ({
        organizations: archived
          ? state.organizations.filter((o) => o.id !== id)
          : state.organizations.map((o) => (o.id === id ? res.data : o)),
      }));
    }
    return res.ok;
  },

  deleteOrganization: async (id) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/organizations/{id}', { params: { path: { id } } }), false,
    );
    if (res.ok) {
      if (get().selectedOrganizationId === id) rememberSelection(null);
      set((state) => ({
        organizations: state.organizations.filter((o) => o.id !== id),
        selectedOrganizationId:
          state.selectedOrganizationId === id ? null : state.selectedOrganizationId,
      }));
    }
    return res.ok;
  },
}));

/**
 * The customer id fleet reads should narrow by, or undefined for the whole
 * tenant. Read outside React so the device store can pass it on every fetch.
 */
export function selectedOrganizationQuery(): string | undefined {
  return useOrganizationStore.getState().selectedOrganizationId ?? undefined;
}
