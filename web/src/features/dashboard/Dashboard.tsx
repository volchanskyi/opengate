import { useEffect } from 'react';
import { Link } from 'react-router';
import { useDeviceStore, FleetHealth } from '../devices';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useVisibleInterval } from '../../lib/use-visible-interval';

/** How often the dashboard refreshes its rollup while the tab is visible. */
const POLL_MS = 15_000;

const EMPTY_HEALTH = { anomalous: 0, watch: 0, healthy: 0, unknown: 0 };

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
  const summary = useDeviceStore((s) => s.summary);
  const fetchSummary = useDeviceStore((s) => s.fetchSummary);

  useEffect(() => {
    fireAndForget(fetchSummary());
  }, [fetchSummary]);

  // Every tile reads one fixed-size response, so a refresh costs the same
  // whatever the fleet size. A hidden tab polls nothing and catches up on
  // re-show.
  useVisibleInterval(() => { fireAndForget(fetchSummary()); }, POLL_MS);

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <h2 className="text-xl font-bold">Dashboard</h2>

      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
        <StatCard label="Total Devices" value={summary?.total ?? 0} to="/devices"
          colorClasses="border-l-4 border-l-blue-500 bg-blue-900/10" />
        <StatCard label="Online" value={summary?.online ?? 0} to="/devices?status=online"
          colorClasses="border-l-4 border-l-green-500 bg-green-900/10" />
        <StatCard label="Offline" value={summary?.offline ?? 0} to="/devices?status=offline"
          colorClasses="border-l-4 border-l-amber-500 bg-amber-900/10" />
        <StatCard label="In Maintenance" value={summary?.maintenance ?? 0} to="/devices?maintenance=true"
          colorClasses="border-l-4 border-l-sky-500 bg-sky-900/10" />
      </div>

      <FleetHealth counts={summary?.health ?? EMPTY_HEALTH} />
    </div>
  );
}
