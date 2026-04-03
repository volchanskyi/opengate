import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
import type { components } from '../types/api';

type Device = components['schemas']['Device'];
type Group = components['schemas']['Group'];
type DeviceHardware = components['schemas']['DeviceHardware'];
type DeviceLogsResponse = components['schemas']['DeviceLogsResponse'];

interface DeviceState {
  devices: Device[];
  groups: Group[];
  selectedGroupId: string | null;
  selectedDevice: Device | null;
  hardware: DeviceHardware | null;
  logs: DeviceLogsResponse | null;
  logsLoading: boolean;
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
  fetchLogs: (id: string, params?: { level?: string; from?: string; to?: string; search?: string; offset?: number; limit?: number }) => Promise<void>;
  upgradeAgent: (deviceId: string, version: string, os: string, arch: string) => Promise<boolean>;
}

export const useDeviceStore = create<DeviceState>((set, get) => ({
  devices: [],
  groups: [],
  selectedGroupId: null,
  selectedDevice: null,
  hardware: null,
  logs: null,
  logsLoading: false,
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

  fetchLogs: async (id, params) => {
    set({ logsLoading: true });
    const query: Record<string, string | number> = {};
    if (params?.level) query.level = params.level;
    if (params?.from) query.from = params.from;
    if (params?.to) query.to = params.to;
    if (params?.search) query.search = params.search;
    if (params?.offset !== undefined) query.offset = params.offset;
    if (params?.limit !== undefined) query.limit = params.limit;

    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}/logs', {
        params: { path: { id }, query },
      }), false,
    );
    if (res.ok) {
      set({ logs: res.data, logsLoading: false });
    } else {
      // 202 (logs requested) — retry once after 3s
      set({ logsLoading: true });
      setTimeout(async () => {
        const retry = await apiAction(set, () =>
          api.GET('/api/v1/devices/{id}/logs', {
            params: { path: { id }, query },
          }), false,
        );
        if (retry.ok) set({ logs: retry.data });
        set({ logsLoading: false });
      }, 3000);
    }
  },

  upgradeAgent: async (deviceId, version, os, arch) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/updates/push', {
        body: { version, os, arch, device_ids: [deviceId] },
      }), false,
    );
    return res.ok;
  },
}));
