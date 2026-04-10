import { useEffect } from 'react';
import { Link } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useAuthStore } from '../../state/auth-store';
import { useAdminStore } from '../../state/admin-store';
import { fireAndForget } from '../../lib/fire-and-forget';

function StatCard({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="bg-gray-800 border border-gray-700 rounded-lg p-4">
      <p className="text-sm text-gray-400">{label}</p>
      <p className="text-2xl font-bold mt-1">{value}</p>
    </div>
  );
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
        <StatCard label="Total Devices" value={devices.length} />
        <StatCard label="Online" value={onlineCount} />
        <StatCard label="Device Groups" value={groups.length} />
        <StatCard label="Offline" value={devices.length - onlineCount} />
      </div>

      <div className="flex gap-3">
        <Link to="/devices" className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded text-sm">
          View All Devices
        </Link>
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
