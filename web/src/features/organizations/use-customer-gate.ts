import { useEffect } from 'react';
import { useOrganizationStore } from './state/organization-store';

/**
 * The chosen customer for a host or site list, or null under "All customers". While a caller is
 * mounted the customer picker is marked, so the reader sees where the missing choice is made.
 */
export function useCustomerGate(): string | null {
  const wantCustomer = useOrganizationStore((s) => s.wantCustomer);
  useEffect(() => wantCustomer(), [wantCustomer]);
  return useOrganizationStore((s) => s.selectedOrganizationId);
}
