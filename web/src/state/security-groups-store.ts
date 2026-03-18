import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
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
    const res = await apiAction(set, () => api.GET('/api/v1/security-groups'));
    if (res.ok) set({ groups: res.data });
  },

  fetchGroupDetail: async (id) => {
    const res = await apiAction(set, () =>
      api.GET('/api/v1/security-groups/{id}', { params: { path: { id } } }),
    );
    if (res.ok) set({ selectedGroup: res.data });
  },

  fetchUsers: async () => {
    const res = await apiAction(set, () => api.GET('/api/v1/users'), false);
    if (res.ok) set({ users: res.data });
  },

  addMember: async (groupId, userId) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/security-groups/{id}/members', {
        params: { path: { id: groupId } },
        body: { user_id: userId },
      }), false,
    );
    if (res.ok) await get().fetchGroupDetail(groupId);
  },

  removeMember: async (groupId, userId) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/security-groups/{id}/members/{userId}', {
        params: { path: { id: groupId, userId } },
      }), false,
    );
    if (res.ok) await get().fetchGroupDetail(groupId);
  },
}));
