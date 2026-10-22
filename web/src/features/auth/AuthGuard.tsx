import { Navigate, Outlet } from 'react-router';
import { useAuthStore } from '../../state/auth-store';
import { IdleSignOut } from './IdleSignOut';

/** Shows the console to a signed-in technician; App has asked the server first. */
export function AuthGuard() {
  const user = useAuthStore((s) => s.user);

  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return (
    <>
      <IdleSignOut />
      <Outlet />
    </>
  );
}
