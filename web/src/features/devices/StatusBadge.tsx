import type { components } from '../../types/api';

type DeviceStatus = components['schemas']['Device']['status'];

const config: Record<DeviceStatus, { color: string; label: string }> = {
  online: { color: 'bg-green-500', label: 'Online' },
  offline: { color: 'bg-gray-500', label: 'Offline' },
  connecting: { color: 'bg-yellow-500', label: 'Connecting' },
};

export function StatusBadge({ status }: Readonly<{ status: DeviceStatus }>) {
  const { color, label } = config[status] ?? { color: 'bg-gray-500', label: status };
  return (
    <span className="inline-flex items-center gap-1.5 text-sm">
      <span className={`w-2 h-2 rounded-full ${color}`} />
      {label}
    </span>
  );
}
