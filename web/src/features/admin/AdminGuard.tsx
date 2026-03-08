import { Navigate, Outlet } from 'react-router-dom';
import { useAuthStore } from '../../state/auth-store';

export function AdminGuard() {
  const user = useAuthStore((s) => s.user);

  if (!user?.is_admin) {
    return <Navigate to="/devices" replace />;
  }

  return <Outlet />;
}
