import { useState } from 'react';
import { useAuthStore } from '../../state/auth-store';
import { useToastStore } from '../../state/toast-store';
import { api } from '../../lib/api';

export function ProfilePage() {
  const user = useAuthStore((s) => s.user);
  const fetchMe = useAuthStore((s) => s.fetchMe);
  const addToast = useToastStore((s) => s.addToast);
  const [displayName, setDisplayName] = useState(user?.display_name ?? '');
  const [saving, setSaving] = useState(false);

  if (!user) return null;

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    const { error } = await api.PATCH('/api/v1/users/{id}', {
      params: { path: { id: user.id } },
      body: { display_name: displayName },
    });
    setSaving(false);
    if (error) {
      addToast(error.error, 'error');
    } else {
      addToast('Profile updated', 'success');
      await fetchMe();
    }
  };

  return (
    <div className="p-6 max-w-lg">
      <h2 className="text-xl font-bold mb-4">Profile</h2>
      <form onSubmit={handleSave} className="bg-gray-800 border border-gray-700 rounded-lg p-6 space-y-4">
        <div>
          <label className="block text-sm text-gray-400 mb-1">Email</label>
          <p className="text-sm font-mono bg-gray-900 border border-gray-700 rounded px-3 py-2">{user.email}</p>
        </div>
        <div>
          <label className="block text-sm text-gray-400 mb-1">Display Name</label>
          <input
            type="text"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            className="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm"
          />
        </div>
        <div>
          <label className="block text-sm text-gray-400 mb-1">Member Since</label>
          <p className="text-sm text-gray-300">{new Date(user.created_at).toLocaleDateString()}</p>
        </div>
        <button
          type="submit"
          disabled={saving}
          className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 rounded text-sm"
        >
          {saving ? 'Saving...' : 'Save'}
        </button>
      </form>
    </div>
  );
}
