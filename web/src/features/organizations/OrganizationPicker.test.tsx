import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useOrganizationStore } from './state/organization-store';
import { OrganizationPicker } from './OrganizationPicker';

vi.mock('../../lib/api', () => ({
  api: { GET: vi.fn().mockResolvedValue({ data: [], response: { ok: true } }) },
}));

const contoso = { id: 'org-1', name: 'Contoso', created_at: '', updated_at: '' };
const fabrikam = { id: 'org-2', name: 'Fabrikam', created_at: '', updated_at: '' };

const fetchOrganizations = vi.fn().mockResolvedValue(undefined);
const selectOrganization = vi.fn();
const hydrateSelection = vi.fn();

describe('OrganizationPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useOrganizationStore.setState({
      organizations: [contoso, fabrikam],
      selectedOrganizationId: null,
      fetchOrganizations,
      selectOrganization,
      hydrateSelection,
    });
  });

  it('renders nothing until the tenant has customers', () => {
    useOrganizationStore.setState({ organizations: [] });
    const { container } = render(<OrganizationPicker />);
    expect(container.querySelector('select')).toBeNull();
  });

  it('offers every customer plus the whole tenant', () => {
    render(<OrganizationPicker />);
    const options = screen.getAllByRole('option').map((o) => o.textContent);
    expect(options).toEqual(['All customers', 'Contoso', 'Fabrikam']);
  });

  it('restores the remembered choice and loads the list on mount', () => {
    render(<OrganizationPicker />);
    expect(hydrateSelection).toHaveBeenCalled();
    expect(fetchOrganizations).toHaveBeenCalled();
  });

  it('shows the remembered choice as selected', () => {
    useOrganizationStore.setState({ selectedOrganizationId: 'org-2' });
    render(<OrganizationPicker />);
    expect(screen.getByLabelText('Customer')).toHaveValue('org-2');
  });

  it('publishes the picked customer', () => {
    render(<OrganizationPicker />);
    fireEvent.change(screen.getByLabelText('Customer'), { target: { value: 'org-2' } });
    expect(selectOrganization).toHaveBeenCalledWith('org-2');
  });

  it('choosing the whole tenant clears the selection', () => {
    useOrganizationStore.setState({ selectedOrganizationId: 'org-1' });
    render(<OrganizationPicker />);
    fireEvent.change(screen.getByLabelText('Customer'), { target: { value: '' } });

    expect(selectOrganization).toHaveBeenCalledWith(null);
  });
});
