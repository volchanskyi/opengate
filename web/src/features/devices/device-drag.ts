/**
 * The private MIME type carrying a dragged device id; `DataTransfer.types` is the only field a
 * `dragover` handler can read.
 */
export const DEVICE_DRAG_MIME = 'application/x-opengate-device';

/**
 * The placeholder site id meaning "no site": `PATCH /devices/{id}` clears the device's site when
 * it receives this, and the Devices page picks it to list the devices filed under no site.
 */
export const NOT_ASSIGNED_SITE_ID = '00000000-0000-0000-0000-000000000000';

/** Publish a dragged device: the id for drop zones, the hostname as the label. */
export function startDeviceDrag(transfer: DataTransfer, device: { id: string; hostname: string }): void {
  transfer.setData(DEVICE_DRAG_MIME, device.id);
  transfer.setData('text/plain', device.hostname);
  transfer.effectAllowed = 'move';
}

/** Whether an in-flight drag carries a device card. */
export function isDeviceDrag(transfer: DataTransfer | null): boolean {
  return transfer ? [...transfer.types].includes(DEVICE_DRAG_MIME) : false;
}

/** The dropped device id, or an empty string for any other kind of drag. */
export function readDraggedDeviceId(transfer: DataTransfer | null): string {
  return transfer?.getData(DEVICE_DRAG_MIME).trim() ?? '';
}
