import { create } from 'zustand';
import { api } from '../lib/api';
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
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/updates/manifests');
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ manifests: data, isLoading: false });
  },

  publishManifest: async (body) => {
    set({ error: null });
    const { error } = await api.POST('/api/v1/updates/manifests', { body });
    if (error) {
      set({ error: error.error });
      return;
    }
    await get().fetchManifests();
  },

  pushUpdate: async (body) => {
    set({ error: null });
    const { data, error } = await api.POST('/api/v1/updates/push', { body });
    if (error) {
      set({ error: error.error });
      return undefined;
    }
    return data.pushed_count;
  },

  fetchEnrollmentTokens: async () => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/enrollment-tokens');
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ enrollmentTokens: data, isLoading: false });
  },

  createEnrollmentToken: async (body) => {
    set({ error: null });
    const { error } = await api.POST('/api/v1/enrollment-tokens', { body });
    if (error) {
      set({ error: error.error });
      return;
    }
    await get().fetchEnrollmentTokens();
  },

  deleteEnrollmentToken: async (id) => {
    set({ error: null });
    const { error } = await api.DELETE('/api/v1/enrollment-tokens/{id}', {
      params: { path: { id } },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    await get().fetchEnrollmentTokens();
  },

  fetchCACert: async () => {
    set({ error: null });
    const { data, error } = await api.GET('/api/v1/server/ca');
    if (error) {
      set({ error: error.error });
      return;
    }
    set({ caPem: data.pem });
  },
}));
