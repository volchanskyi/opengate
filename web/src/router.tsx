/* eslint-disable react-refresh/only-export-components */
import { lazy, Suspense } from 'react';
import { createBrowserRouter, Navigate } from 'react-router-dom';
import { LoginPage } from './features/auth/LoginPage';
import { RegisterPage } from './features/auth/RegisterPage';
import { AuthGuard } from './features/auth/AuthGuard';
import { AdminGuard } from './features/admin/AdminGuard';
import { Layout } from './components/Layout';
import { LoadingSpinner } from './components/LoadingSpinner';

const Dashboard = lazy(() => import('./features/dashboard/Dashboard').then((m) => ({ default: m.Dashboard })));
const DeviceList = lazy(() => import('./features/devices/DeviceList').then((m) => ({ default: m.DeviceList })));
const DeviceDetail = lazy(() => import('./features/devices/DeviceDetail').then((m) => ({ default: m.DeviceDetail })));
const SessionView = lazy(() => import('./features/session/SessionView').then((m) => ({ default: m.SessionView })));
const AgentSetupPage = lazy(() => import('./features/agent-setup/AgentSetupPage').then((m) => ({ default: m.AgentSetupPage })));
const ProfilePage = lazy(() => import('./features/profile/ProfilePage').then((m) => ({ default: m.ProfilePage })));
const AdminLayout = lazy(() => import('./features/admin/AdminLayout').then((m) => ({ default: m.AdminLayout })));
const UserManagement = lazy(() => import('./features/admin/UserManagement').then((m) => ({ default: m.UserManagement })));
const AuditLog = lazy(() => import('./features/admin/AuditLog').then((m) => ({ default: m.AuditLog })));
const AgentUpdates = lazy(() => import('./features/admin/AgentUpdates').then((m) => ({ default: m.AgentUpdates })));
const Permissions = lazy(() => import('./features/admin/Permissions').then((m) => ({ default: m.Permissions })));

function withSuspense(Component: React.LazyExoticComponent<React.ComponentType>) {
  return (
    <Suspense fallback={<LoadingSpinner />}>
      <Component />
    </Suspense>
  );
}

export const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  { path: '/register', element: <RegisterPage /> },
  {
    path: '/',
    element: <AuthGuard />,
    children: [
      {
        element: <Layout />,
        children: [
          { index: true, element: withSuspense(Dashboard) },
          { path: 'devices', element: withSuspense(DeviceList) },
          { path: 'devices/:id', element: withSuspense(DeviceDetail) },
          { path: 'sessions/:token', element: withSuspense(SessionView) },
          { path: 'setup', element: withSuspense(AgentSetupPage) },
          { path: 'profile', element: withSuspense(ProfilePage) },
          {
            path: 'settings',
            element: <AdminGuard />,
            children: [
              {
                element: withSuspense(AdminLayout),
                children: [
                  { index: true, element: <Navigate to="/settings/users" replace /> },
                  { path: 'users', element: withSuspense(UserManagement) },
                  { path: 'audit', element: withSuspense(AuditLog) },
                  { path: 'updates', element: withSuspense(AgentUpdates) },
                  { path: 'security/permissions', element: withSuspense(Permissions) },
                ],
              },
            ],
          },
          // Redirect old /admin routes to /settings
          { path: 'admin/*', element: <Navigate to="/settings" replace /> },
        ],
      },
    ],
  },
]);
