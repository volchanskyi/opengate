import { useNavigate } from 'react-router-dom';
import type { components } from '../../types/api';
import { StatusBadge } from './StatusBadge';
import { HealthBadge } from './HealthBadge';
import { useInventoryStore } from './state/inventory-store';
import { fireAndForget } from '../../lib/fire-and-forget';

type Device = components['schemas']['Device'];

function timeAgo(dateStr: string): string {
  const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${String(minutes)}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${String(hours)}h ago`;
  const days = Math.floor(hours / 24);
  return `${String(days)}d ago`;
}

export function DeviceCard({ device }: Readonly<{ device: Device }>) {
  const navigate = useNavigate();
  const inventory = useInventoryStore((s) => s.byDevice.get(device.id));

  const serviceCount = inventory?.filter((i) => i.kind === 'service').length ?? 0;
  const containerCount = inventory?.filter((i) => i.kind === 'container').length ?? 0;
  const showHint = inventory !== undefined && (serviceCount > 0 || containerCount > 0);

  return (
    <button
      type="button"
      onClick={() => { fireAndForget(navigate(`/devices/${device.id}`)); }}
      className="w-full text-left bg-gray-800 border border-gray-700 rounded-lg p-4 hover:border-gray-500 transition-colors"
    >
      <div className="flex items-center justify-between mb-2">
        <h3 className="font-medium truncate">{device.hostname}</h3>
        <StatusBadge status={device.status} />
      </div>
      <div className="text-sm text-gray-400 space-y-1">
        <p>OS: {device.os_display || device.os}</p>
        <p>Last seen: {timeAgo(device.last_seen)}</p>
        {showHint && (
          <p className="text-xs text-gray-500">
            Discovered: {serviceCount} service{serviceCount !== 1 ? 's' : ''} · {containerCount} container{containerCount !== 1 ? 's' : ''}
          </p>
        )}
        {device.anomaly_rate != null && (
          <HealthBadge anomalyRate={device.anomaly_rate} />
        )}
      </div>
    </button>
  );
}
