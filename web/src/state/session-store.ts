import { create } from 'zustand';
import { api } from '../lib/api';
import type { components } from '../types/api';

type AgentSession = components['schemas']['AgentSession'];

interface SessionState {
  sessions: AgentSession[];
  isLoading: boolean;
  error: string | null;
  fetchSessions: (deviceId: string) => Promise<void>;
  createSession: (deviceId: string) => Promise<{ token: string; relay_url: string } | null>;
  deleteSession: (token: string) => Promise<void>;
}

export const useSessionStore = create<SessionState>((set) => ({
  sessions: [],
  isLoading: false,
  error: null,

  fetchSessions: async (deviceId) => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/sessions', {
      params: { query: { device_id: deviceId } },
    });
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ sessions: data, isLoading: false });
  },

  createSession: async (deviceId) => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.POST('/api/v1/sessions', {
      body: { device_id: deviceId },
    });
    if (error) {
      set({ isLoading: false, error: error.error });
      return null;
    }
    set({ isLoading: false });
    return data;
  },

  deleteSession: async (token) => {
    set({ error: null });
    const { error } = await api.DELETE('/api/v1/sessions/{token}', {
      params: { path: { token } },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    set((state) => ({
      sessions: state.sessions.filter((s) => s.token !== token),
    }));
  },
}));
