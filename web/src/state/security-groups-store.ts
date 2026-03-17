import { create } from 'zustand';
import { api } from '../lib/api';
import type { components } from '../types/api';

type SecurityGroup = components['schemas']['SecurityGroup'];
type SecurityGroupWithMembers = components['schemas']['SecurityGroupWithMembers'];
type User = components['schemas']['User'];

interface SecurityGroupsState {
  groups: SecurityGroup[];
  selectedGroup: SecurityGroupWithMembers | null;
  users: User[];
  isLoading: boolean;
  error: string | null;
  fetchGroups: () => Promise<void>;
  fetchGroupDetail: (id: string) => Promise<void>;
  fetchUsers: () => Promise<void>;
  addMember: (groupId: string, userId: string) => Promise<void>;
  removeMember: (groupId: string, userId: string) => Promise<void>;
}

export const useSecurityGroupsStore = create<SecurityGroupsState>((set, get) => ({
  groups: [],
  selectedGroup: null,
  users: [],
  isLoading: false,
  error: null,

  fetchGroups: async () => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/security-groups');
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ groups: data, isLoading: false });
  },

  fetchGroupDetail: async (id) => {
    set({ isLoading: true, error: null });
    const { data, error } = await api.GET('/api/v1/security-groups/{id}', {
      params: { path: { id } },
    });
    if (error) {
      set({ isLoading: false, error: error.error });
      return;
    }
    set({ selectedGroup: data, isLoading: false });
  },

  fetchUsers: async () => {
    const { data, error } = await api.GET('/api/v1/users');
    if (error) {
      set({ error: error.error });
      return;
    }
    set({ users: data });
  },

  addMember: async (groupId, userId) => {
    set({ error: null });
    const { error } = await api.POST('/api/v1/security-groups/{id}/members', {
      params: { path: { id: groupId } },
      body: { user_id: userId },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    await get().fetchGroupDetail(groupId);
  },

  removeMember: async (groupId, userId) => {
    set({ error: null });
    const { error } = await api.DELETE('/api/v1/security-groups/{id}/members/{userId}', {
      params: { path: { id: groupId, userId } },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    await get().fetchGroupDetail(groupId);
  },
}));
