import { NavLink, Outlet } from 'react-router-dom';

const navSections = [
  {
    label: 'Management',
    items: [
      { to: '/settings/users', label: 'Users' },
      { to: '/settings/audit', label: 'Audit Log' },
      { to: '/settings/updates', label: 'Agent Updates' },
    ],
  },
  {
    label: 'Security',
    items: [
      { to: '/settings/security/permissions', label: 'Permissions' },
    ],
  },
];

export function AdminLayout() {
  return (
    <div className="flex h-[calc(100vh-3.25rem)]">
      <aside className="w-48 bg-gray-800 border-r border-gray-700 p-4 flex flex-col gap-1">
        {navSections.map((section) => (
          <div key={section.label} className="mb-3">
            <p className="text-xs uppercase text-gray-500 font-semibold px-3 mb-1">
              {section.label}
            </p>
            {section.items.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                className={({ isActive }) =>
                  `block px-3 py-2 rounded text-sm ${isActive ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white hover:bg-gray-750'}`
                }
              >
                {item.label}
              </NavLink>
            ))}
          </div>
        ))}
      </aside>
      <div className="flex-1 overflow-auto p-6">
        <Outlet />
      </div>
    </div>
  );
}
