import { useEffect, useState } from 'react';
import { useSecurityGroupsStore } from '../../state/security-groups-store';
import { useAuthStore } from '../../state/auth-store';

export function Permissions() {
  const groups = useSecurityGroupsStore((s) => s.groups);
  const selectedGroup = useSecurityGroupsStore((s) => s.selectedGroup);
  const users = useSecurityGroupsStore((s) => s.users);
  const isLoading = useSecurityGroupsStore((s) => s.isLoading);
  const error = useSecurityGroupsStore((s) => s.error);
  const fetchGroups = useSecurityGroupsStore((s) => s.fetchGroups);
  const fetchGroupDetail = useSecurityGroupsStore((s) => s.fetchGroupDetail);
  const fetchUsers = useSecurityGroupsStore((s) => s.fetchUsers);
  const addMember = useSecurityGroupsStore((s) => s.addMember);
  const removeMember = useSecurityGroupsStore((s) => s.removeMember);
  const currentUser = useAuthStore((s) => s.user);

  const [selectedUserId, setSelectedUserId] = useState('');

  useEffect(() => {
    void fetchGroups();
    void fetchUsers();
  }, [fetchGroups, fetchUsers]);

  // Auto-select the first group when groups load.
  useEffect(() => {
    const first = groups[0];
    if (first && !selectedGroup) {
      void fetchGroupDetail(first.id);
    }
  }, [groups, selectedGroup, fetchGroupDetail]);

  const memberIds = new Set(selectedGroup?.members?.map((m) => m.id) ?? []);
  const nonMembers = users.filter((u) => !memberIds.has(u.id));
  const isLastAdmin = selectedGroup?.members?.length === 1;

  const handleAdd = async () => {
    if (!selectedGroup || !selectedUserId) return;
    await addMember(selectedGroup.id, selectedUserId);
    setSelectedUserId('');
  };

  const handleRemove = async (userId: string) => {
    if (!selectedGroup) return;
    await removeMember(selectedGroup.id, userId);
  };

  if (isLoading && groups.length === 0) {
    return <p className="text-gray-400">Loading security groups...</p>;
  }

  return (
    <div>
      <h2 className="text-xl font-bold mb-4">Permissions</h2>

      {error && (
        <div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* Group tabs */}
      <div className="flex gap-2 mb-6">
        {groups.map((group) => (
          <button
            key={group.id}
            onClick={() => { void fetchGroupDetail(group.id); }}
            className={`px-3 py-1.5 rounded text-sm flex items-center gap-2 ${
              selectedGroup?.id === group.id
                ? 'bg-blue-600 text-white'
                : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
            }`}
          >
            {group.name}
            {group.is_system && (
              <span className="text-xs bg-gray-600 px-1.5 py-0.5 rounded">System</span>
            )}
          </button>
        ))}
      </div>

      {selectedGroup && (
        <div className="bg-gray-800 rounded-lg p-4">
          <div className="mb-4">
            <h3 className="text-lg font-semibold">{selectedGroup.name}</h3>
            {selectedGroup.description && (
              <p className="text-gray-400 text-sm mt-1">{selectedGroup.description}</p>
            )}
          </div>

          {/* Add member */}
          <div className="flex gap-2 mb-4">
            <select
              value={selectedUserId}
              onChange={(e) => setSelectedUserId(e.target.value)}
              className="flex-1 bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm"
            >
              <option value="">Select user to add...</option>
              {nonMembers.map((user) => (
                <option key={user.id} value={user.id}>
                  {user.email} {user.display_name ? `(${user.display_name})` : ''}
                </option>
              ))}
            </select>
            <button
              onClick={() => { void handleAdd(); }}
              disabled={!selectedUserId}
              className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              Add Member
            </button>
          </div>

          {/* Members table */}
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-700 text-left text-gray-400">
                <th className="pb-2">Email</th>
                <th className="pb-2">Display Name</th>
                <th className="pb-2">Actions</th>
              </tr>
            </thead>
            <tbody>
              {selectedGroup.members?.map((member) => (
                <tr key={member.id} className="border-b border-gray-800">
                  <td className="py-2">{member.email}</td>
                  <td className="py-2">{member.display_name || '-'}</td>
                  <td className="py-2">
                    <button
                      onClick={() => { void handleRemove(member.id); }}
                      disabled={isLastAdmin && member.id === currentUser?.id}
                      title={
                        isLastAdmin && member.id === currentUser?.id
                          ? 'Cannot remove the last administrator'
                          : 'Remove from group'
                      }
                      className="text-red-400 hover:text-red-300 text-xs disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                      Remove
                    </button>
                  </td>
                </tr>
              ))}
              {(!selectedGroup.members || selectedGroup.members.length === 0) && (
                <tr>
                  <td colSpan={3} className="py-4 text-center text-gray-500">
                    No members in this group
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
