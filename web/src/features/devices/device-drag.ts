/**
 * Payload contract for dragging a device card onto a site in the sidebar.
 *
 * The id travels under a private MIME type rather than `text/plain` so a drop
 * zone can tell a device drag from arbitrary text (a selection dragged in from
 * another window) using only `DataTransfer.types` — the sole field readable
 * from a `dragover` handler, where the browser hides the payload itself.
 */
export const DEVICE_DRAG_MIME = 'application/x-opengate-device';

/**
 * The placeholder site id meaning "no site". `PATCH /devices/{id}` clears the
 * device's site when it receives this, and the detail pane renders it as N/A.
 */
export const UNFILED_SITE_ID = '00000000-0000-0000-0000-000000000000';

/** Publish a dragged device: the id for drop zones, the hostname as the label. */
export function startDeviceDrag(transfer: DataTransfer, device: { id: string; hostname: string }): void {
  transfer.setData(DEVICE_DRAG_MIME, device.id);
  transfer.setData('text/plain', device.hostname);
  transfer.effectAllowed = 'move';
}

/** Whether an in-flight drag carries one of our device cards. */
export function isDeviceDrag(transfer: DataTransfer | null): boolean {
  return transfer ? [...transfer.types].includes(DEVICE_DRAG_MIME) : false;
}

/** The dropped device id, or an empty string for any other kind of drag. */
export function readDraggedDeviceId(transfer: DataTransfer | null): string {
  return transfer?.getData(DEVICE_DRAG_MIME).trim() ?? '';
}
