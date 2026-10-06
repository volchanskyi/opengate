import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction } from '../../../state/api-action';
import { selectedOrganizationQuery } from '../../organizations';
import type { components } from '../../../types/api';

type AlertLimits = components['schemas']['AlertLimits'];

/** A customer's alert budget, returned with the maxima the server allows beside each value. */
interface AlertLimitsState {
  limits: AlertLimits | null;
  isLoading: boolean;
  error: string | null;

  fetchLimits: () => Promise<void>;
  saveLimits: (organizationHourly: number, deviceHourly: number) => Promise<boolean>;
}

function customerQuery(): { organization_id?: string } {
  const organizationId = selectedOrganizationQuery();
  return organizationId ? { organization_id: organizationId } : {};
}

export const useAlertLimitsStore = create<AlertLimitsState>((set) => ({
  limits: null,
  isLoading: false,
  error: null,

  fetchLimits: async () => {
    const res = await apiAction(set, () =>
      api.GET('/api/v1/alert-limits', { params: { query: customerQuery() } }),
    );
    if (!res.ok) return;
    set({ limits: res.data });
  },

  saveLimits: async (organizationHourly, deviceHourly) => {
    const res = await apiAction(set, () =>
      api.PUT('/api/v1/alert-limits', {
        params: { query: customerQuery() },
        body: { organization_hourly: organizationHourly, device_hourly: deviceHourly },
      }), false);
    if (!res.ok) return false;
    set({ limits: res.data });
    return true;
  },
}));
