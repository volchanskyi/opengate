import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
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
    const res = await apiAction(set, () => api.GET('/api/v1/users'));
    if (res.ok) set({ users: res.data });
  },

  updateUser: async (id, body) => {
    const res = await apiAction(set, () =>
      api.PATCH('/api/v1/users/{id}', { params: { path: { id } }, body }), false,
    );
    if (res.ok) await get().fetchUsers();
  },

  deleteUser: async (id) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/users/{id}', { params: { path: { id } } }), false,
    );
    if (res.ok) await get().fetchUsers();
  },

  fetchAuditEvents: async (filters = {}) => {
    const res = await apiAction(set, () =>
      api.GET('/api/v1/audit', { params: { query: filters } }),
    );
    if (res.ok) set({ auditEvents: res.data });
  },
}));
