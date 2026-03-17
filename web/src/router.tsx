import { createBrowserRouter, Navigate } from 'react-router-dom';
import { LoginPage } from './features/auth/LoginPage';
import { RegisterPage } from './features/auth/RegisterPage';
import { AuthGuard } from './features/auth/AuthGuard';
import { AdminGuard } from './features/admin/AdminGuard';
import { AdminLayout } from './features/admin/AdminLayout';
import { UserManagement } from './features/admin/UserManagement';
import { AuditLog } from './features/admin/AuditLog';
import { AgentUpdates } from './features/admin/AgentUpdates';
import { Permissions } from './features/admin/Permissions';
import { Layout } from './components/Layout';
import { DeviceList } from './features/devices/DeviceList';
import { DeviceDetail } from './features/devices/DeviceDetail';
import { SessionView } from './features/session/SessionView';
import { AgentSetupPage } from './features/agent-setup/AgentSetupPage';

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
          { index: true, element: <Navigate to="/devices" replace /> },
          { path: 'devices', element: <DeviceList /> },
          { path: 'devices/:id', element: <DeviceDetail /> },
          { path: 'sessions/:token', element: <SessionView /> },
          { path: 'setup', element: <AgentSetupPage /> },
          {
            path: 'admin',
            element: <AdminGuard />,
            children: [
              {
                element: <AdminLayout />,
                children: [
                  { index: true, element: <Navigate to="/admin/users" replace /> },
                  { path: 'users', element: <UserManagement /> },
                  { path: 'audit', element: <AuditLog /> },
                  { path: 'updates', element: <AgentUpdates /> },
                  { path: 'security/permissions', element: <Permissions /> },
                ],
              },
            ],
          },
        ],
      },
    ],
  },
]);
