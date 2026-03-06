import { create } from 'zustand';
import { api } from '../lib/api';
import type { components } from '../types/api';

type User = components['schemas']['User'];

interface AuthState {
  token: string | null;
  user: User | null;
  isLoading: boolean;
  error: string | null;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, displayName: string) => Promise<void>;
  logout: () => void;
  fetchMe: () => Promise<void>;
  hydrate: () => Promise<void>;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: null,
  user: null,
  isLoading: false,
  error: null,

  login: async (email, password) => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.POST('/api/v1/auth/login', {
      body: { email, password },
    });
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    localStorage.setItem('token', data.token);
    set({ token: data.token, isLoading: false });
    await get().fetchMe();
  },

  register: async (email, password, displayName) => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.POST('/api/v1/auth/register', {
      body: { email, password, display_name: displayName },
    });
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    localStorage.setItem('token', data.token);
    set({ token: data.token, isLoading: false });
    await get().fetchMe();
  },

  logout: () => {
    localStorage.removeItem('token');
    set({ token: null, user: null, error: null });
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
    if (!token) return;
    set({ token });
    await get().fetchMe();
  },
}));
