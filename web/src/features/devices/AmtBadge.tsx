import type { components } from '../../types/api';

type DeviceAMT = components['schemas']['DeviceAMT'];

interface AmtBadgeProps {
  /** The device's AMT property, straight off the device payload. */
  readonly amt: DeviceAMT | undefined;
  readonly className?: string;
}

/** Tooltip suffix describing the CIRA connection behind an AMT-capable device. */
function connectionLabel(status: DeviceAMT['status']): string {
  if (status === 'online') return 'online';
  if (status === 'offline') return 'offline';
  return 'not connected';
}

/**
 * "Intel AMT" pill shown beside a device's status.
 *
 * It keys off `available` — the agent's Management Engine reading — so it stays
 * put whether or not an AMT connection happens to be up; only the tooltip tracks
 * connection state. A device that has a linked connection also qualifies, so the
 * badge does not blink out while a fresh agent's first hardware report is still
 * in flight.
 */
export function AmtBadge({ amt, className = '' }: AmtBadgeProps) {
  if (!amt || (!amt.available && !amt.uuid)) return null;

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 text-xs font-medium bg-blue-900/40 text-blue-300 border border-blue-700 ${className}`}
      title={`Intel AMT · ${connectionLabel(amt.status)}`}
    >
      Intel AMT
    </span>
  );
}
