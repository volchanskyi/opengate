import { Outlet } from 'react-router-dom';
import { useAuthStore } from '../state/auth-store';

export function Layout() {
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <nav className="bg-gray-800 border-b border-gray-700 px-4 py-3 flex items-center justify-between">
        <h1 className="text-lg font-bold">OpenGate</h1>
        <div className="flex items-center gap-4">
          {user && <span className="text-sm text-gray-300">{user.display_name || user.email}</span>}
          <button
            onClick={logout}
            className="text-sm text-gray-400 hover:text-white"
          >
            Logout
          </button>
        </div>
      </nav>
      <main>
        <Outlet />
      </main>
    </div>
  );
}
