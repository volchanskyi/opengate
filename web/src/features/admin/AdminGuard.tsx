import { Navigate, Outlet } from 'react-router-dom';
import { useAuthStore } from '../../state/auth-store';

export function AdminGuard() {
  const user = useAuthStore((s) => s.user);
  const hydrated = useAuthStore((s) => s.hydrated);

  // Wait for hydrate() to populate the user from localStorage + /users/me.
  // Without this, every fresh navigation to /settings/* would render once
  // with user=null and trigger an immediate Navigate, even for valid admins
  // — see AdminGuard.test.tsx regression case.
  if (!hydrated) {
    return null;
  }

  if (!user?.is_admin) {
    return <Navigate to="/devices" replace />;
  }

  return <Outlet />;
}
