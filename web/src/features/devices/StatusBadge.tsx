import type { components } from '../../types/api';

type DeviceStatus = components['schemas']['Device']['status'];

function statusDisplay(status: DeviceStatus): { color: string; label: string } {
  switch (status) {
    case 'online':
      return { color: 'bg-green-500', label: 'Online' };
    case 'offline':
      return { color: 'bg-gray-500', label: 'Offline' };
    case 'connecting':
      return { color: 'bg-yellow-500', label: 'Connecting' };
    default:
      return { color: 'bg-gray-500', label: status };
  }
}

export function StatusBadge({ status }: Readonly<{ status: DeviceStatus }>) {
  const { color, label } = statusDisplay(status);
  return (
    <span className="inline-flex items-center gap-1.5 text-sm">
      <span className={`w-2 h-2 rounded-full ${color}`} />
      {label}
    </span>
  );
}
