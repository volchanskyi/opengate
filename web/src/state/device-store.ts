import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
import type { components } from '../types/api';

type Device = components['schemas']['Device'];
type Group = components['schemas']['Group'];
type DeviceHardware = components['schemas']['DeviceHardware'];

interface DeviceState {
  devices: Device[];
  groups: Group[];
  selectedGroupId: string | null;
  selectedDevice: Device | null;
  hardware: DeviceHardware | null;
  isLoading: boolean;
  error: string | null;
  fetchGroups: () => Promise<void>;
  fetchDevices: (groupId?: string) => Promise<void>;
  fetchDevice: (id: string) => Promise<void>;
  selectGroup: (id: string | null) => void;
  createGroup: (name: string) => Promise<void>;
  deleteGroup: (id: string) => Promise<void>;
  deleteDevice: (id: string) => Promise<void>;
  updateDeviceGroup: (id: string, groupId: string) => Promise<boolean>;
  restartAgent: (id: string) => Promise<boolean>;
  fetchHardware: (id: string) => Promise<void>;
}

export const useDeviceStore = create<DeviceState>((set, get) => ({
  devices: [],
  groups: [],
  selectedGroupId: null,
  selectedDevice: null,
  hardware: null,
  isLoading: false,
  error: null,

  fetchGroups: async () => {
    const res = await apiAction(set, () => api.GET('/api/v1/groups'));
    if (res.ok) set({ groups: res.data });
  },

  fetchDevices: async (groupId?) => {
    const query = groupId ? { group_id: groupId } : {};
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices', { params: { query } }),
    );
    if (res.ok) set({ devices: res.data });
  },

  fetchDevice: async (id) => {
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}', { params: { path: { id } } }),
    );
    if (res.ok) set({ selectedDevice: res.data });
  },

  selectGroup: (id) => {
    set({ selectedGroupId: id });
    get().fetchDevices(id ?? undefined);
  },

  createGroup: async (name) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/groups', { body: { name } }), false,
    );
    if (res.ok) set((state) => ({ groups: [...state.groups, res.data] }));
  },

  deleteGroup: async (id) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/groups/{id}', { params: { path: { id } } }), false,
    );
    if (res.ok) {
      set((state) => ({
        groups: state.groups.filter((g) => g.id !== id),
        selectedGroupId: state.selectedGroupId === id ? null : state.selectedGroupId,
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

  updateDeviceGroup: async (id, groupId) => {
    const res = await apiAction(set, () =>
      api.PATCH('/api/v1/devices/{id}', {
        params: { path: { id } },
        body: { group_id: groupId },
      }), false,
    );
    if (res.ok) {
      set({ selectedDevice: res.data });
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

  fetchHardware: async (id) => {
    set({ hardware: null });
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}/hardware', {
        params: { path: { id } },
      }), false,
    );
    if (res.ok) {
      set({ hardware: res.data });
    } else {
      // 202 (report requested) or 404 — retry once after 2s in case the agent responds
      setTimeout(async () => {
        const retry = await apiAction(set, () =>
          api.GET('/api/v1/devices/{id}/hardware', {
            params: { path: { id } },
          }), false,
        );
        if (retry.ok) set({ hardware: retry.data });
      }, 2000);
    }
  },
}));
