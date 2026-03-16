import { NavLink, Outlet } from 'react-router-dom';

const navItems = [
  { to: '/admin/users', label: 'Users' },
  { to: '/admin/audit', label: 'Audit Log' },
  { to: '/admin/updates', label: 'Agent Updates' },
];

export function AdminLayout() {
  return (
    <div className="flex h-[calc(100vh-3.25rem)]">
      <aside className="w-48 bg-gray-800 border-r border-gray-700 p-4 flex flex-col gap-1">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            className={({ isActive }) =>
              `px-3 py-2 rounded text-sm ${isActive ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white hover:bg-gray-750'}`
            }
          >
            {item.label}
          </NavLink>
        ))}
      </aside>
      <div className="flex-1 overflow-auto p-6">
        <Outlet />
      </div>
    </div>
  );
}
