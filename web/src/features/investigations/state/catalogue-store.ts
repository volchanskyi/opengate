import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction, progressAdapter } from '../../../state/api-action';
import { selectedOrganizationQuery } from '../../organizations';
import type { components } from '../../../types/api';

type Rule = components['schemas']['Rule'];

interface CatalogueState {
  rules: Rule[];
  /** How many machines the coverage counts were taken against. */
  fleetSize: number;
  /** Whether the catalogue has been read, so an empty pack is an answer. */
  loaded: boolean;
  loading: boolean;
  error: string | null;

  fetchCatalogue: () => Promise<void>;
}

export const useCatalogueStore = create<CatalogueState>((set, get) => ({
  rules: [],
  fleetSize: 0,
  loaded: false,
  loading: false,
  error: null,

  fetchCatalogue: async () => {
    if (get().loading) return;
    const organizationId = selectedOrganizationQuery();

    const res = await apiAction(
      progressAdapter(
        (error) => { set({ error }); },
        (loading) => { set({ loading }); },
      ),
      () => api.GET('/api/v1/rules', {
        params: { query: organizationId ? { organization_id: organizationId } : {} },
      }),
    );
    if (!res.ok) return;
    set({ rules: res.data.rules, fleetSize: res.data.fleet_size, loaded: true });
  },
}));
