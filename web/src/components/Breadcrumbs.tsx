import { Link, useLocation, useParams } from 'react-router-dom';
import { useDeviceStore } from '../state/device-store';

interface Crumb {
  label: string;
  to?: string;
}

export function Breadcrumbs() {
  const location = useLocation();
  const params = useParams();
  const device = useDeviceStore((s) => s.selectedDevice);
  const segments = location.pathname.split('/').filter(Boolean);

  if (segments.length === 0) return null;

  const crumbs: Crumb[] = [];
  let path = '';

  for (let i = 0; i < segments.length; i++) {
    const seg = segments[i]!;
    path += `/${seg}`;
    const isLast = i === segments.length - 1;

    if (seg === 'devices' && !segments[i + 1]) {
      crumbs.push(isLast ? { label: 'Devices' } : { label: 'Devices', to: '/devices' });
    } else if (seg === 'devices' && segments[i + 1]) {
      crumbs.push({ label: 'Devices', to: '/devices' });
    } else if (seg === params.id && crumbs.some((c) => c.label === 'Devices')) {
      const label = device?.hostname ?? seg;
      crumbs.push(isLast ? { label } : { label, to: path });
    } else if (seg === 'sessions') {
      crumbs.push(isLast ? { label: 'Session' } : { label: 'Sessions', to: path });
    } else if (seg === params.token) {
      crumbs.push({ label: 'Session' });
    } else if (seg === 'settings') {
      crumbs.push(isLast ? { label: 'Settings' } : { label: 'Settings', to: '/settings' });
    } else if (seg === 'security') {
      // skip, next segment shows the real label
    } else if (seg === 'users') {
      crumbs.push(isLast ? { label: 'Users' } : { label: 'Users', to: path });
    } else if (seg === 'audit') {
      crumbs.push(isLast ? { label: 'Audit Log' } : { label: 'Audit Log', to: path });
    } else if (seg === 'updates') {
      crumbs.push(isLast ? { label: 'Agent Settings' } : { label: 'Agent Settings', to: path });
    } else if (seg === 'permissions') {
      crumbs.push({ label: 'Permissions' });
    } else if (seg === 'setup') {
      crumbs.push({ label: 'Add Device' });
    } else if (seg === 'profile') {
      crumbs.push({ label: 'Profile' });
    }
  }

  if (crumbs.length === 0) return null;

  return (
    <nav className="px-6 py-2 text-sm text-gray-400 flex items-center gap-1">
      <Link to="/" className="hover:text-white">Dashboard</Link>
      {crumbs.map((crumb, i) => (
        <span key={i} className="flex items-center gap-1">
          <span className="mx-1">&gt;</span>
          {crumb.to ? (
            <Link to={crumb.to} className="hover:text-white">{crumb.label}</Link>
          ) : (
            <span className="text-white">{crumb.label}</span>
          )}
        </span>
      ))}
    </nav>
  );
}
