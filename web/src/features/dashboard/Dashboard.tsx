import { useEffect } from 'react';
import { Link } from 'react-router';
import { useDeviceStore, FleetHealth } from '../devices';
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
  const maintenanceCount = useDeviceStore((s) => s.maintenanceCount);
  const fetchDevices = useDeviceStore((s) => s.fetchDevices);
  const fetchMaintenanceSummary = useDeviceStore((s) => s.fetchMaintenanceSummary);

  useEffect(() => {
    fireAndForget(fetchDevices());
    fireAndForget(fetchMaintenanceSummary());
  }, [fetchDevices, fetchMaintenanceSummary]);

  // Poll device status and the maintenance count so the tiles stay current.
  useEffect(() => {
    const interval = setInterval(() => {
      fireAndForget(fetchDevices());
      fireAndForget(fetchMaintenanceSummary());
    }, 15_000);
    return () => clearInterval(interval);
  }, [fetchDevices, fetchMaintenanceSummary]);

  const onlineCount = devices.filter((d) => d.status === 'online').length;

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <h2 className="text-xl font-bold">Dashboard</h2>

      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
        <StatCard label="Total Devices" value={devices.length} to="/devices"
          colorClasses="border-l-4 border-l-blue-500 bg-blue-900/10" />
        <StatCard label="Online" value={onlineCount} to="/devices?status=online"
          colorClasses="border-l-4 border-l-green-500 bg-green-900/10" />
        <StatCard label="Offline" value={devices.length - onlineCount} to="/devices?status=offline"
          colorClasses="border-l-4 border-l-amber-500 bg-amber-900/10" />
        <StatCard label="In Maintenance" value={maintenanceCount} to="/devices?maintenance=true"
          colorClasses="border-l-4 border-l-sky-500 bg-sky-900/10" />
      </div>

      <FleetHealth devices={devices} />
    </div>
  );
}
