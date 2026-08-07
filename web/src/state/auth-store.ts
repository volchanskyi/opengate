import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
import type { components } from '../types/api';

type User = components['schemas']['User'];

interface AuthState {
  token: string | null;
  tenantId: string | null;
  user: User | null;
  isLoading: boolean;
  hydrated: boolean;
  error: string | null;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, displayName: string) => Promise<void>;
  logout: () => void;
  fetchMe: () => Promise<void>;
  hydrate: () => Promise<void>;
}

export function tenantIdFromToken(token: string): string | null {
  const payload = token.split('.')[1];
  if (!payload) {
    return null;
  }
  try {
    const normalized = payload.replace(/-/g, '+').replace(/_/g, '/');
    const padded = normalized.padEnd(normalized.length + ((4 - (normalized.length % 4)) % 4), '=');
    const claims = JSON.parse(globalThis.atob(padded)) as { tenant?: unknown };
    return typeof claims.tenant === 'string' && claims.tenant.length > 0 ? claims.tenant : null;
  } catch {
    return null;
  }
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: null,
  tenantId: null,
  user: null,
  isLoading: false,
  hydrated: false,
  error: null,

  login: async (email, password) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/auth/login', { body: { email, password } }),
    );
    if (res.ok) {
      localStorage.setItem('token', res.data.token);
      set({ token: res.data.token, tenantId: tenantIdFromToken(res.data.token) });
      await get().fetchMe();
    }
  },

  register: async (email, password, displayName) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/auth/register', {
        body: { email, password, display_name: displayName },
      }),
    );
    if (res.ok) {
      localStorage.setItem('token', res.data.token);
      set({ token: res.data.token, tenantId: tenantIdFromToken(res.data.token) });
      await get().fetchMe();
    }
  },

  logout: () => {
    localStorage.removeItem('token');
    set({ token: null, tenantId: null, user: null, error: null });
  },

  fetchMe: async () => {
    const { data, error, response } = await api.GET('/api/v1/users/me');
    if (error) {
      if (response.status === 401) {
        get().logout();
      }
      return;
    }
    set({ user: data });
  },

  hydrate: async () => {
    const token = localStorage.getItem('token');
    if (!token) {
      set({ hydrated: true });
      return;
    }
    set({ token, tenantId: tenantIdFromToken(token) });
    await get().fetchMe();
    set({ hydrated: true });
  },
}));
