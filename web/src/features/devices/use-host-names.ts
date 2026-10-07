import { useEffect, useMemo } from 'react';
import { fireAndForget } from '../../lib/fire-and-forget';
import { usePickListStore, WHOLE_TENANT } from './state/pick-list-store';

/** Host names by id for the chosen customer, or for the whole tenant when none is chosen. */
export function useHostNames(customerId: string | null): ReadonlyMap<string, string> {
  const hosts = usePickListStore((s) => s.hosts.get(customerId ?? WHOLE_TENANT));
  const fetchHosts = usePickListStore((s) => s.fetchHosts);

  useEffect(() => {
    fireAndForget(fetchHosts(customerId));
  }, [customerId, fetchHosts]);

  return useMemo(() => new Map((hosts ?? []).map((h) => [h.id, h.name])), [hosts]);
}
