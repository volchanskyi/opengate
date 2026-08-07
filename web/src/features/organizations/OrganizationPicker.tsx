import { useEffect } from 'react';
import { useOrganizationStore } from './state/organization-store';
import { fireAndForget } from '../../lib/fire-and-forget';

/**
 * Chooses which customer the fleet views describe. A technician sees every
 * customer in the tenant, so without this a thirty-customer device list is one
 * undifferentiated pile.
 *
 * It publishes the choice and nothing more: the device list and the dashboard
 * re-read when it changes, so the tiles and the fleet below them never describe
 * different sets, and the picker stays unaware of either.
 */
export function OrganizationPicker() {
  const organizations = useOrganizationStore((s) => s.organizations);
  const selectedOrganizationId = useOrganizationStore((s) => s.selectedOrganizationId);
  const selectOrganization = useOrganizationStore((s) => s.selectOrganization);
  const fetchOrganizations = useOrganizationStore((s) => s.fetchOrganizations);
  const hydrateSelection = useOrganizationStore((s) => s.hydrateSelection);

  useEffect(() => {
    hydrateSelection();
    fireAndForget(fetchOrganizations());
  }, [hydrateSelection, fetchOrganizations]);

  function onChange(value: string) {
    selectOrganization(value === '' ? null : value);
  }

  if (organizations.length === 0) {
    return null;
  }

  return (
    <label className="flex items-center gap-2 text-sm text-gray-400">
      <span className="sr-only">Customer</span>
      <select
        aria-label="Customer"
        value={selectedOrganizationId ?? ''}
        onChange={(e) => onChange(e.target.value)}
        className="bg-gray-700 text-white text-sm rounded px-2 py-1 border border-gray-600"
      >
        <option value="">All customers</option>
        {organizations.map((o) => (
          <option key={o.id} value={o.id}>
            {o.name}
          </option>
        ))}
      </select>
    </label>
  );
}
