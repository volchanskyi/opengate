/**
 * The leading block of a UUID — enough to tell two rows apart at a glance, and
 * short enough to sit inline in a table or a breadcrumb.
 */
export function shortId(id: string | null | undefined): string {
  if (!id) return '—';
  return id.slice(0, 8);
}
