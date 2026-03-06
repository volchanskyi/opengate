import { createBrowserRouter, Navigate } from 'react-router-dom';
import { LoginPage } from './features/auth/LoginPage';
import { RegisterPage } from './features/auth/RegisterPage';
import { AuthGuard } from './features/auth/AuthGuard';
import { Layout } from './components/Layout';
import { DeviceList } from './features/devices/DeviceList';
import { DeviceDetail } from './features/devices/DeviceDetail';

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
        ],
      },
    ],
  },
]);
