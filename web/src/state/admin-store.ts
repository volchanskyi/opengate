import { create } from 'zustand';
import { api } from '../lib/api';
import type { components } from '../types/api';

type User = components['schemas']['User'];
type AuditEvent = components['schemas']['AuditEvent'];

interface AuditFilters {
  user_id?: string;
  action?: string;
  limit?: number;
  offset?: number;
}

interface AdminState {
  users: User[];
  auditEvents: AuditEvent[];
  isLoading: boolean;
  error: string | null;
  fetchUsers: () => Promise<void>;
  updateUser: (id: string, body: { is_admin?: boolean; display_name?: string }) => Promise<void>;
  deleteUser: (id: string) => Promise<void>;
  fetchAuditEvents: (filters?: AuditFilters) => Promise<void>;
}

export const useAdminStore = create<AdminState>((set, get) => ({
  users: [],
  auditEvents: [],
  isLoading: false,
  error: null,

  fetchUsers: async () => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/users');
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ users: data, isLoading: false });
  },

  updateUser: async (id, body) => {
    set({ error: null });
    const { error } = await api.PATCH('/api/v1/users/{id}', {
      params: { path: { id } },
      body,
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    await get().fetchUsers();
  },

  deleteUser: async (id) => {
    set({ error: null });
    const { error } = await api.DELETE('/api/v1/users/{id}', {
      params: { path: { id } },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    await get().fetchUsers();
  },

  fetchAuditEvents: async (filters = {}) => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/audit', {
      params: { query: filters },
    });
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ auditEvents: data, isLoading: false });
  },
}));
