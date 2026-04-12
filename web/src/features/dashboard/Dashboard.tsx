import { useEffect } from 'react';
import { Link } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useAuthStore } from '../../state/auth-store';
import { useAdminStore } from '../../state/admin-store';
import { fireAndForget } from '../../lib/fire-and-forget';

interface StatCardProps {
  readonly label: string;
  readonly value: number | string;
  readonly to?: string;
  readonly colorClasses?: string;
}

function StatCard({ label, value, to, colorClasses = '' }: StatCardProps) {
  const base = `bg-gray-800 border border-gray-700 rounded-lg p-4 ${colorClasses}`;
  const content = (
    <>
      <p className="text-sm text-gray-400">{label}</p>
      <p className="text-2xl font-bold mt-1">{value}</p>
    </>
  );

  if (to) {
    return (
      <Link to={to} className={`${base} hover:bg-gray-700 transition-colors block`}>
        {content}
      </Link>
    );
  }
  return <div className={base}>{content}</div>;
}

export function Dashboard() {
  const devices = useDeviceStore((s) => s.devices);
  const groups = useDeviceStore((s) => s.groups);
  const fetchDevices = useDeviceStore((s) => s.fetchDevices);
  const fetchGroups = useDeviceStore((s) => s.fetchGroups);
  const user = useAuthStore((s) => s.user);
  const auditEvents = useAdminStore((s) => s.auditEvents);
  const fetchAuditEvents = useAdminStore((s) => s.fetchAuditEvents);

  useEffect(() => {
    fireAndForget(fetchDevices());
    fireAndForget(fetchGroups());
    if (user?.is_admin) {
      fireAndForget(fetchAuditEvents({ limit: 10 }));
    }
  }, [fetchDevices, fetchGroups, fetchAuditEvents, user?.is_admin]);

  // Poll device status so online/offline counts stay current.
  useEffect(() => {
    const interval = setInterval(() => { fireAndForget(fetchDevices()); }, 15_000);
    return () => clearInterval(interval);
  }, [fetchDevices]);

  const onlineCount = devices.filter((d) => d.status === 'online').length;

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <h2 className="text-xl font-bold">Dashboard</h2>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatCard label="Total Devices" value={devices.length} to="/devices"
          colorClasses="border-l-4 border-l-blue-500 bg-blue-900/10" />
        <StatCard label="Online" value={onlineCount}
          colorClasses="border-l-4 border-l-green-500 bg-green-900/10" />
        <StatCard label="Device Groups" value={groups.length}
          colorClasses="border-l-4 border-l-indigo-500 bg-indigo-900/10" />
        <StatCard label="Offline" value={devices.length - onlineCount}
          colorClasses="border-l-4 border-l-amber-500 bg-amber-900/10" />
      </div>

      <div className="flex gap-3">
        <Link to="/setup" className="px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded text-sm">
          Add Device
        </Link>
      </div>

      {user?.is_admin && auditEvents.length > 0 && (
        <section>
          <h3 className="text-lg font-semibold mb-3">Recent Activity</h3>
          <div className="bg-gray-800 border border-gray-700 rounded-lg overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-700 text-left text-gray-400">
                  <th className="px-4 py-2">Action</th>
                  <th className="px-4 py-2">Target</th>
                  <th className="px-4 py-2">Time</th>
                </tr>
              </thead>
              <tbody>
                {auditEvents.slice(0, 10).map((event) => (
                  <tr key={event.id} className="border-b border-gray-800">
                    <td className="px-4 py-2 font-mono text-xs">{event.action}</td>
                    <td className="px-4 py-2 text-gray-400 text-xs truncate max-w-[200px]">
                      {event.target || '\u2014'}
                    </td>
                    <td className="px-4 py-2 text-gray-400 text-xs">
                      {new Date(event.created_at).toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  );
}
