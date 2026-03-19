import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
import type { components } from '../types/api';

type AMTDevice = components['schemas']['AMTDevice'];
type PowerAction = components['schemas']['AMTPowerRequest']['action'];

interface AMTState {
  amtDevices: AMTDevice[];
  selectedAmtDevice: AMTDevice | null;
  isLoading: boolean;
  error: string | null;
  fetchAmtDevices: () => Promise<void>;
  fetchAmtDevice: (uuid: string) => Promise<void>;
  sendPowerAction: (uuid: string, action: PowerAction) => Promise<boolean>;
}

export const useAMTStore = create<AMTState>((set) => ({
  amtDevices: [],
  selectedAmtDevice: null,
  isLoading: false,
  error: null,

  fetchAmtDevices: async () => {
    const res = await apiAction(set, () => api.GET('/api/v1/amt/devices'));
    if (res.ok) set({ amtDevices: res.data });
  },

  fetchAmtDevice: async (uuid) => {
    const res = await apiAction(set, () =>
      api.GET('/api/v1/amt/devices/{uuid}', { params: { path: { uuid } } }),
    );
    if (res.ok) set({ selectedAmtDevice: res.data });
  },

  sendPowerAction: async (uuid, action) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/amt/devices/{uuid}/power', {
        params: { path: { uuid } },
        body: { action },
      }), false,
    );
    return res.ok;
  },
}));
