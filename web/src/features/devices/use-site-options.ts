import { useEffect } from 'react';
import { fireAndForget } from '../../lib/fire-and-forget';
import { usePickListStore, type SiteOption } from './state/pick-list-store';

const NONE: readonly SiteOption[] = [];

/** One customer's sites for a pick-list, sorted by name; none until a customer is chosen. */
export function useSiteOptions(customerId: string | null): readonly SiteOption[] {
  const sites = usePickListStore((s) => (customerId ? s.sites.get(customerId) : undefined));
  const fetchSites = usePickListStore((s) => s.fetchSites);

  useEffect(() => {
    if (customerId) fireAndForget(fetchSites(customerId));
  }, [customerId, fetchSites]);

  return sites ?? NONE;
}
