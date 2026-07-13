import { create } from 'zustand';
import { api } from '../../../lib/api';
import type { components } from '../../../types/api';

type InventoryItem = components['schemas']['InventoryItem'];

interface InventoryState {
  /** Discovered footprint per device id. Absent key means "not yet fetched". */
  byDevice: Map<string, InventoryItem[]>;
  loading: Map<string, boolean>;
  errors: Map<string, string>;
  /**
   * Load a device's inventory. Cache-first by default so the grid can trigger a
   * fetch per visible card without hammering the API; pass `force` to bypass the
   * cache (the detail view's Refresh button).
   */
  fetchInventory: (id: string, force?: boolean) => Promise<void>;
}

export const useInventoryStore = create<InventoryState>((set, get) => ({
  byDevice: new Map(),
  loading: new Map(),
  errors: new Map(),

  fetchInventory: async (id, force = false) => {
    const { byDevice, loading } = get();
    if (loading.get(id)) return;
    if (!force && byDevice.has(id)) return;

    set((s) => {
      const errors = new Map(s.errors);
      errors.delete(id);
      return { loading: new Map(s.loading).set(id, true), errors };
    });

    const { data } = await api.GET('/api/v1/devices/{id}/inventory', { params: { path: { id } } });

    if (data) {
      set((s) => ({
        byDevice: new Map(s.byDevice).set(id, data.items),
        loading: new Map(s.loading).set(id, false),
      }));
      return;
    }

    set((s) => ({
      loading: new Map(s.loading).set(id, false),
      errors: new Map(s.errors).set(id, 'Failed to load inventory.'),
    }));
  },
}));
