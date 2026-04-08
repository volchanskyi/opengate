import { useEffect } from 'react';
import { useAdminStore } from '../../state/admin-store';
import { useAuthStore } from '../../state/auth-store';

export function UserManagement() {
  const users = useAdminStore((s) => s.users);
  const isLoading = useAdminStore((s) => s.isLoading);
  const fetchUsers = useAdminStore((s) => s.fetchUsers);
  const updateUser = useAdminStore((s) => s.updateUser);
  const deleteUser = useAdminStore((s) => s.deleteUser);
  const currentUser = useAuthStore((s) => s.user);

  useEffect(() => {
    void fetchUsers();
  }, [fetchUsers]);

  if (isLoading && users.length === 0) {
    return <p className="text-gray-400">Loading users...</p>;
  }

  return (
    <div>
      <h2 className="text-xl font-bold mb-4">User Management</h2>
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-700 text-left text-gray-400">
            <th className="pb-2">Email</th>
            <th className="pb-2">Display Name</th>
            <th className="pb-2">Admin</th>
            <th className="pb-2">Actions</th>
          </tr>
        </thead>
        <tbody>
          {users.map((user) => (
            <tr key={user.id} className="border-b border-gray-800">
              <td className="py-2">{user.email}</td>
              <td className="py-2">{user.display_name}</td>
              <td className="py-2">
                <button
                  onClick={() => { void updateUser(user.id, { is_admin: !user.is_admin }); }}
                  disabled={user.id === currentUser?.id}
                  className={`px-2 py-0.5 rounded text-xs ${
                    user.is_admin
                      ? 'bg-green-900 text-green-300'
                      : 'bg-gray-700 text-gray-400'
                  } ${user.id === currentUser?.id ? 'opacity-50 cursor-not-allowed' : 'hover:opacity-80'}`}
                >
                  {user.is_admin ? 'Yes' : 'No'}
                </button>
              </td>
              <td className="py-2">
                <button
                  onClick={() => { void deleteUser(user.id); }}
                  disabled={user.id === currentUser?.id}
                  className="text-red-400 hover:text-red-300 text-xs disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
