import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction } from '../../../state/api-action';
import { selectedOrganizationQuery } from '../../organizations';
import type { components } from '../../../types/api';

type DeviceTagLabel = components['schemas']['DeviceTagLabel'];
type DeviceTagAssignment = components['schemas']['DeviceTagAssignment'];

/**
 * A customer's label list and who carries what.
 *
 * Deleting a label is not a free action: a rule aimed at it loses a tuned value
 * on every machine that carried it, which widens a threshold across an estate
 * without anything saying so. The server refuses that, and the message it
 * refuses with is what this surfaces.
 */
interface DeviceTagsState {
  labels: DeviceTagLabel[];
  assignments: DeviceTagAssignment[];
  isLoading: boolean;
  error: string | null;

  fetchTags: () => Promise<void>;
  createLabel: (key: string, value: string) => Promise<boolean>;
  deleteLabel: (labelId: string) => Promise<boolean>;
  assignLabel: (labelId: string, deviceIds: readonly string[]) => Promise<boolean>;
  clearTag: (deviceId: string, key: string) => Promise<boolean>;
}

function customerQuery(): { organization_id?: string } {
  const organizationId = selectedOrganizationQuery();
  return organizationId ? { organization_id: organizationId } : {};
}

export const useDeviceTagsStore = create<DeviceTagsState>((set, get) => ({
  labels: [],
  assignments: [],
  isLoading: false,
  error: null,

  fetchTags: async () => {
    const res = await apiAction(set, () =>
      api.GET('/api/v1/device-tags', { params: { query: customerQuery() } }),
    );
    if (!res.ok) return;
    set({ labels: res.data.labels, assignments: res.data.assignments });
  },

  createLabel: async (key, value) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/device-tags/labels', {
        params: { query: customerQuery() },
        body: { key, value },
      }), false);
    if (!res.ok) return false;
    await get().fetchTags();
    return true;
  },

  deleteLabel: async (labelId) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/device-tags/labels/{label_id}', {
        params: { path: { label_id: labelId } },
      }), false);
    if (!res.ok) return false;
    await get().fetchTags();
    return true;
  },

  assignLabel: async (labelId, deviceIds) => {
    const res = await apiAction(set, () =>
      api.PUT('/api/v1/device-tags/assignments', {
        body: { label_id: labelId, device_ids: [...deviceIds] },
      }), false);
    if (!res.ok) return false;
    await get().fetchTags();
    return true;
  },

  clearTag: async (deviceId, key) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/device-tags/assignments', {
        params: { query: { device_id: deviceId, key } },
      }), false);
    if (!res.ok) return false;
    await get().fetchTags();
    return true;
  },
}));
