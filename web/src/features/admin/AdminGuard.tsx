import { Navigate, Outlet } from 'react-router';
import { useAuthStore } from '../../state/auth-store';

export function AdminGuard() {
  const user = useAuthStore((s) => s.user);
  const hydrated = useAuthStore((s) => s.hydrated);

  // Renders nothing until hydrate() has loaded the user, so a valid admin is not
  // redirected on the first render.
  if (!hydrated) {
    return null;
  }

  if (!user?.is_admin) {
    return <Navigate to="/devices" replace />;
  }

  return <Outlet />;
}
