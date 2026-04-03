import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
import type { components } from '../types/api';

type AgentManifest = components['schemas']['AgentManifest'];
type EnrollmentToken = components['schemas']['EnrollmentToken'];

interface UpdateState {
  manifests: AgentManifest[];
  enrollmentTokens: EnrollmentToken[];
  isLoading: boolean;
  error: string | null;
  fetchManifests: () => Promise<void>;
  fetchEnrollmentTokens: () => Promise<void>;
  createEnrollmentToken: (body: components['schemas']['CreateEnrollmentTokenRequest']) => Promise<void>;
  deleteEnrollmentToken: (id: string) => Promise<void>;
}

export const useUpdateStore = create<UpdateState>((set, get) => ({
  manifests: [],
  enrollmentTokens: [],
  isLoading: false,
  error: null,

  fetchManifests: async () => {
    const res = await apiAction(set, () => api.GET('/api/v1/updates/manifests'));
    if (res.ok) set({ manifests: res.data });
  },

  fetchEnrollmentTokens: async () => {
    const res = await apiAction(set, () => api.GET('/api/v1/enrollment-tokens'));
    if (res.ok) set({ enrollmentTokens: res.data });
  },

  createEnrollmentToken: async (body) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/enrollment-tokens', { body }), false,
    );
    if (res.ok) await get().fetchEnrollmentTokens();
  },

  deleteEnrollmentToken: async (id) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/enrollment-tokens/{id}', { params: { path: { id } } }), false,
    );
    if (res.ok) await get().fetchEnrollmentTokens();
  },
}));
