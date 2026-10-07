import { useEffect } from 'react';
import type { HostOption } from '../../components/HostSelect';
import { fireAndForget } from '../../lib/fire-and-forget';
import { usePickListStore } from './state/pick-list-store';

const NONE: readonly HostOption[] = [];

/** One customer's hosts for a pick-list, sorted by name; none until a customer is chosen. */
export function useHostOptions(customerId: string | null): readonly HostOption[] {
  const hosts = usePickListStore((s) => (customerId ? s.hosts.get(customerId) : undefined));
  const fetchHosts = usePickListStore((s) => s.fetchHosts);

  useEffect(() => {
    if (customerId) fireAndForget(fetchHosts(customerId));
  }, [customerId, fetchHosts]);

  return hosts ?? NONE;
}
