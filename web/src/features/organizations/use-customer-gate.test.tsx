import { act, render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useOrganizationStore } from './state/organization-store';
import { OrganizationPicker } from './OrganizationPicker';
import { useCustomerGate } from './use-customer-gate';

vi.mock('../../lib/api', () => ({
  api: { GET: vi.fn().mockResolvedValue({ data: [], response: { ok: true } }) },
}));

const contoso = { id: 'org-1', name: 'Contoso', created_at: '', updated_at: '' };

function HostList() {
  const customer = useCustomerGate();
  return <p>{customer ?? 'Pick a customer first'}</p>;
}

function Page({ showList }: { readonly showList: boolean }) {
  return (
    <>
      <OrganizationPicker />
      {showList && <HostList />}
    </>
  );
}

describe('useCustomerGate', () => {
  beforeEach(() => {
    useOrganizationStore.setState({
      organizations: [contoso],
      selectedOrganizationId: null,
      fetchOrganizations: vi.fn().mockResolvedValue(undefined),
      hydrateSelection: vi.fn(),
    });
  });

  it('marks the customer picker while a customer-bound list waits on it', () => {
    render(<Page showList />);
    expect(screen.getByLabelText('Customer')).toHaveClass('ring-2');
    expect(screen.getByText('Pick a customer first')).toBeInTheDocument();
  });

  it('leaves the picker plain when no list on screen needs a customer', () => {
    render(<Page showList={false} />);
    expect(screen.getByLabelText('Customer')).not.toHaveClass('ring-2');
  });

  it('stops marking the picker once the list leaves the screen', () => {
    const { rerender } = render(<Page showList />);
    rerender(<Page showList={false} />);
    expect(screen.getByLabelText('Customer')).not.toHaveClass('ring-2');
  });

  it('hands the list the chosen customer and stops marking the picker', () => {
    render(<Page showList />);
    act(() => { useOrganizationStore.getState().selectOrganization('org-1'); });
    expect(screen.getByText('org-1')).toBeInTheDocument();
    expect(screen.getByLabelText('Customer')).not.toHaveClass('ring-2');
  });

  it('keeps the picker marked while any one of several lists still waits', () => {
    const { rerender } = render(<><OrganizationPicker /><HostList /><HostList /></>);
    rerender(<><OrganizationPicker /><HostList /></>);
    expect(screen.getByLabelText('Customer')).toHaveClass('ring-2');
  });
});
