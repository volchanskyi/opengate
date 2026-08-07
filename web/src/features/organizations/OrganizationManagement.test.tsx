import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useOrganizationStore } from './state/organization-store';
import { OrganizationManagement } from './OrganizationManagement';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], response: { ok: true } }),
    POST: vi.fn(),
    PATCH: vi.fn(),
    DELETE: vi.fn(),
  },
}));

const contoso = { id: 'org-1', name: 'Contoso', created_at: '', updated_at: '' };
const retired = { id: 'org-2', name: 'Northwind', archived_at: '2026-08-06T00:00:00Z', created_at: '', updated_at: '' };

const fetchOrganizations = vi.fn().mockResolvedValue(undefined);
const createOrganization = vi.fn().mockResolvedValue(true);
const renameOrganization = vi.fn().mockResolvedValue(true);
const setOrganizationArchived = vi.fn().mockResolvedValue(true);
const deleteOrganization = vi.fn().mockResolvedValue(true);

describe('OrganizationManagement', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useOrganizationStore.setState({
      organizations: [contoso],
      selectedOrganizationId: null,
      isLoading: false,
      error: null,
      fetchOrganizations,
      createOrganization,
      renameOrganization,
      setOrganizationArchived,
      deleteOrganization,
    });
  });

  it('lists the tenant customers', () => {
    render(<OrganizationManagement />);
    expect(screen.getByText('Contoso')).toBeInTheDocument();
  });

  it('adds a customer and clears the field', async () => {
    render(<OrganizationManagement />);
    fireEvent.change(screen.getByLabelText('New customer name'), { target: { value: 'Fabrikam' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add customer' }));

    await waitFor(() => expect(createOrganization).toHaveBeenCalledWith('Fabrikam'));
    await waitFor(() => expect(screen.getByLabelText('New customer name')).toHaveValue(''));
  });

  it('does not submit a blank name', async () => {
    render(<OrganizationManagement />);
    fireEvent.click(screen.getByRole('button', { name: 'Add customer' }));
    await waitFor(() => expect(createOrganization).not.toHaveBeenCalled());
  });

  it('renames a customer', async () => {
    render(<OrganizationManagement />);
    fireEvent.click(screen.getByRole('button', { name: 'Rename' }));
    fireEvent.change(screen.getByLabelText('Rename Contoso'), { target: { value: 'Contoso Ltd' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(renameOrganization).toHaveBeenCalledWith('org-1', 'Contoso Ltd'));
  });

  it('retires a live customer', async () => {
    render(<OrganizationManagement />);
    fireEvent.click(screen.getByRole('button', { name: 'Retire' }));
    await waitFor(() => expect(setOrganizationArchived).toHaveBeenCalledWith('org-1', true));
  });

  it('restores a retired customer, which is labelled as retired', async () => {
    useOrganizationStore.setState({ organizations: [retired] });
    render(<OrganizationManagement />);
    expect(screen.getByText('Retired')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Restore' }));
    await waitFor(() => expect(setOrganizationArchived).toHaveBeenCalledWith('org-2', false));
  });

  it('asks before deleting, and does nothing when the answer is no', async () => {
    const confirmSpy = vi.spyOn(globalThis, 'confirm').mockReturnValue(false);
    render(<OrganizationManagement />);
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(confirmSpy).toHaveBeenCalled());
    expect(deleteOrganization).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it('deletes when the answer is yes', async () => {
    const confirmSpy = vi.spyOn(globalThis, 'confirm').mockReturnValue(true);
    render(<OrganizationManagement />);
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(deleteOrganization).toHaveBeenCalledWith('org-1'));
    confirmSpy.mockRestore();
  });

  it('asks the server for retired customers when the box is ticked', async () => {
    render(<OrganizationManagement />);
    fireEvent.click(screen.getByLabelText('Show retired customers'));
    await waitFor(() => expect(fetchOrganizations).toHaveBeenLastCalledWith(true));
  });

  it('shows the server error', () => {
    useOrganizationStore.setState({ error: 'a customer of that name already exists in this tenant' });
    render(<OrganizationManagement />);
    expect(screen.getByText(/already exists/)).toBeInTheDocument();
  });
});
