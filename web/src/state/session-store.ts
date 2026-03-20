import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
import type { components } from '../types/api';

type AgentSession = components['schemas']['AgentSession'];

interface SessionState {
  sessions: AgentSession[];
  isLoading: boolean;
  error: string | null;
  fetchSessions: (deviceId: string) => Promise<void>;
  createSession: (deviceId: string) => Promise<{ token: string; relay_url: string; ice_servers?: components['schemas']['ICEServer'][] } | null>;
  deleteSession: (token: string) => Promise<void>;
}

export const useSessionStore = create<SessionState>((set) => ({
  sessions: [],
  isLoading: false,
  error: null,

  fetchSessions: async (deviceId) => {
    // Clear stale sessions from a previous device immediately.
    set({ sessions: [] });
    const res = await apiAction(set, () =>
      api.GET('/api/v1/sessions', { params: { query: { device_id: deviceId } } }),
    );
    if (res.ok) set({ sessions: res.data });
  },

  createSession: async (deviceId) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/sessions', {
        body: {
          device_id: deviceId,
          permissions: {
            desktop: true,
            terminal: true,
            file_read: true,
            file_write: true,
            input: true,
          },
        },
      }),
    );
    return res.ok ? res.data : null;
  },

  deleteSession: async (token) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/sessions/{token}', { params: { path: { token } } }), false,
    );
    if (res.ok) {
      set((state) => ({
        sessions: state.sessions.filter((s) => s.token !== token),
      }));
    }
  },
}));
