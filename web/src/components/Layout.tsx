import { Link, Outlet } from 'react-router-dom';
import { useAuthStore } from '../state/auth-store';
import { NotificationCenter } from '../features/admin/NotificationCenter';
import { ToastContainer } from './ToastContainer';
import { Breadcrumbs } from './Breadcrumbs';

export function Layout() {
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <nav className="bg-gray-800 border-b border-gray-700 px-4 py-3 flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Link to="/" className="text-lg font-bold hover:text-blue-400">
            OpenGate
          </Link>
          <Link to="/devices" className="text-sm text-gray-400 hover:text-white">
            Devices
          </Link>
          {user?.is_admin && (
            <Link to="/settings" className="text-sm text-gray-400 hover:text-white">
              Settings
            </Link>
          )}
        </div>
        <div className="flex items-center gap-4">
          <NotificationCenter />
          {user && (
            <Link to="/profile" className="text-sm text-gray-300 hover:text-white">
              {user.display_name || user.email}
            </Link>
          )}
          <button
            onClick={logout}
            className="text-sm text-gray-400 hover:text-white"
          >
            Logout
          </button>
        </div>
      </nav>
      <Breadcrumbs />
      <main>
        <Outlet />
      </main>
      <ToastContainer />
    </div>
  );
}
