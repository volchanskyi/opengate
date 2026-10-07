import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { fireAndForget } from './fire-and-forget';

/** The label a page hands its breadcrumb, read from the history entry's state; text only. */
export function routeCrumbOf(state: unknown): string | undefined {
  if (typeof state !== 'object' || state === null) return undefined;
  const crumb = new Map(Object.entries(state)).get('crumb');
  return typeof crumb === 'string' ? crumb : undefined;
}

/**
 * Writes the page's own label into its history entry, replacing the entry in place, so the
 * breadcrumb names the page without reading any page's store.
 */
export function useRouteCrumb(label: string | undefined): void {
  const location = useLocation();
  const navigate = useNavigate();

  useEffect(() => {
    if (!label || routeCrumbOf(location.state) === label) return;
    const kept = typeof location.state === 'object' && location.state !== null ? location.state : {};
    fireAndForget(navigate(location, { replace: true, state: { ...kept, crumb: label } }));
  }, [label, location, navigate]);
}
