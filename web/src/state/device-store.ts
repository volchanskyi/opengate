import { create } from 'zustand';
import { api } from '../lib/api';
import type { components } from '../types/api';

type Device = components['schemas']['Device'];
type Group = components['schemas']['Group'];

interface DeviceState {
  devices: Device[];
  groups: Group[];
  selectedGroupId: string | null;
  selectedDevice: Device | null;
  isLoading: boolean;
  error: string | null;
  fetchGroups: () => Promise<void>;
  fetchDevices: (groupId: string) => Promise<void>;
  fetchDevice: (id: string) => Promise<void>;
  selectGroup: (id: string | null) => void;
  createGroup: (name: string) => Promise<void>;
  deleteGroup: (id: string) => Promise<void>;
  deleteDevice: (id: string) => Promise<void>;
}

export const useDeviceStore = create<DeviceState>((set, get) => ({
  devices: [],
  groups: [],
  selectedGroupId: null,
  selectedDevice: null,
  isLoading: false,
  error: null,

  fetchGroups: async () => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/groups');
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ groups: data, isLoading: false });
  },

  fetchDevices: async (groupId) => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/devices', {
      params: { query: { group_id: groupId } },
    });
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ devices: data, isLoading: false });
  },

  fetchDevice: async (id) => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/devices/{id}', {
      params: { path: { id } },
    });
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ selectedDevice: data, isLoading: false });
  },

  selectGroup: (id) => {
    set({ selectedGroupId: id });
    if (id) {
      get().fetchDevices(id);
    }
  },

  createGroup: async (name) => {
    set({ error: null });
    const { data, error } = await api.POST('/api/v1/groups', {
      body: { name },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    set((state) => ({ groups: [...state.groups, data] }));
  },

  deleteGroup: async (id) => {
    set({ error: null });
    const { error } = await api.DELETE('/api/v1/groups/{id}', {
      params: { path: { id } },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    set((state) => ({
      groups: state.groups.filter((g) => g.id !== id),
      selectedGroupId: state.selectedGroupId === id ? null : state.selectedGroupId,
    }));
  },

  deleteDevice: async (id) => {
    set({ error: null });
    const { error } = await api.DELETE('/api/v1/devices/{id}', {
      params: { path: { id } },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    set((state) => ({
      devices: state.devices.filter((d) => d.id !== id),
    }));
  },
}));
