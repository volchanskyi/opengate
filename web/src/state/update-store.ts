import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
import type { components } from '../types/api';

type AgentManifest = components['schemas']['AgentManifest'];
type EnrollmentToken = components['schemas']['EnrollmentToken'];

interface UpdateState {
  manifests: AgentManifest[];
  enrollmentTokens: EnrollmentToken[];
  caPem: string | null;
  isLoading: boolean;
  error: string | null;
  fetchManifests: () => Promise<void>;
  publishManifest: (body: components['schemas']['PublishUpdateRequest']) => Promise<void>;
  pushUpdate: (body: components['schemas']['PushUpdateRequest']) => Promise<number | undefined>;
  fetchEnrollmentTokens: () => Promise<void>;
  createEnrollmentToken: (body: components['schemas']['CreateEnrollmentTokenRequest']) => Promise<void>;
  deleteEnrollmentToken: (id: string) => Promise<void>;
  fetchCACert: () => Promise<void>;
}

export const useUpdateStore = create<UpdateState>((set, get) => ({
  manifests: [],
  enrollmentTokens: [],
  caPem: null,
  isLoading: false,
  error: null,

  fetchManifests: async () => {
    const res = await apiAction(set, () => api.GET('/api/v1/updates/manifests'));
    if (res.ok) set({ manifests: res.data });
  },

  publishManifest: async (body) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/updates/manifests', { body }), false,
    );
    if (res.ok) await get().fetchManifests();
  },

  pushUpdate: async (body) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/updates/push', { body }), false,
    );
    return res.ok ? res.data.pushed_count : undefined;
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

  fetchCACert: async () => {
    const res = await apiAction(set, () => api.GET('/api/v1/server/ca'), false);
    if (res.ok) set({ caPem: res.data.pem });
  },
}));
