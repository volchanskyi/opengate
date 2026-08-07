import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useOrganizationStore, SELECTED_ORGANIZATION_KEY } from './organization-store';

const mockGet = vi.fn();
const mockPost = vi.fn();
const mockPatch = vi.fn();
const mockDelete = vi.fn();

vi.mock('../../../lib/api', () => ({
  api: {
    GET: (...args: unknown[]) => mockGet(...args),
    POST: (...args: unknown[]) => mockPost(...args),
    PATCH: (...args: unknown[]) => mockPatch(...args),
    DELETE: (...args: unknown[]) => mockDelete(...args),
  },
}));

const contoso = { id: 'org-1', name: 'Contoso', created_at: '', updated_at: '' };
const fabrikam = { id: 'org-2', name: 'Fabrikam', created_at: '', updated_at: '' };

const ok = <T,>(data: T) => ({ data, response: { ok: true } as Response });
const failed = { error: { error: 'nope' }, response: { ok: false, status: 409 } as Response };

describe('organization store', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useOrganizationStore.setState({
      organizations: [],
      selectedOrganizationId: null,
      isLoading: false,
      error: null,
    });
  });

  it('fetches the tenant customers', async () => {
    mockGet.mockResolvedValue(ok([contoso, fabrikam]));
    await useOrganizationStore.getState().fetchOrganizations();
    expect(useOrganizationStore.getState().organizations).toEqual([contoso, fabrikam]);
  });

  it('asks for archived customers only when told to', async () => {
    mockGet.mockResolvedValue(ok([]));
    await useOrganizationStore.getState().fetchOrganizations();
    expect(mockGet).toHaveBeenLastCalledWith('/api/v1/organizations', {
      params: { query: {} },
    });

    await useOrganizationStore.getState().fetchOrganizations(true);
    expect(mockGet).toHaveBeenLastCalledWith('/api/v1/organizations', {
      params: { query: { include_archived: true } },
    });
  });

  it('remembers the selected customer across a reload', () => {
    useOrganizationStore.getState().selectOrganization('org-2');
    expect(useOrganizationStore.getState().selectedOrganizationId).toBe('org-2');
    expect(localStorage.getItem(SELECTED_ORGANIZATION_KEY)).toBe('org-2');

    useOrganizationStore.getState().selectOrganization(null);
    expect(useOrganizationStore.getState().selectedOrganizationId).toBeNull();
    expect(localStorage.getItem(SELECTED_ORGANIZATION_KEY)).toBeNull();
  });

  it('drops a remembered customer the tenant no longer has', async () => {
    useOrganizationStore.getState().selectOrganization('org-gone');
    mockGet.mockResolvedValue(ok([contoso]));

    await useOrganizationStore.getState().fetchOrganizations();

    expect(useOrganizationStore.getState().selectedOrganizationId).toBeNull();
    expect(localStorage.getItem(SELECTED_ORGANIZATION_KEY)).toBeNull();
  });

  it('keeps a remembered customer the tenant still has', async () => {
    useOrganizationStore.getState().selectOrganization('org-2');
    mockGet.mockResolvedValue(ok([contoso, fabrikam]));

    await useOrganizationStore.getState().fetchOrganizations();

    expect(useOrganizationStore.getState().selectedOrganizationId).toBe('org-2');
  });

  it('adds a created customer to the list', async () => {
    mockPost.mockResolvedValue(ok(fabrikam));
    useOrganizationStore.setState({ organizations: [contoso] });

    const created = await useOrganizationStore.getState().createOrganization('Fabrikam');

    expect(created).toBe(true);
    expect(mockPost).toHaveBeenCalledWith('/api/v1/organizations', { body: { name: 'Fabrikam' } });
    expect(useOrganizationStore.getState().organizations).toEqual([contoso, fabrikam]);
  });

  it('leaves the list alone when a create is refused', async () => {
    mockPost.mockResolvedValue(failed);
    useOrganizationStore.setState({ organizations: [contoso] });

    const created = await useOrganizationStore.getState().createOrganization('Contoso');

    expect(created).toBe(false);
    expect(useOrganizationStore.getState().organizations).toEqual([contoso]);
    expect(useOrganizationStore.getState().error).toBe('nope');
  });

  it('replaces the renamed customer in place', async () => {
    const renamed = { ...contoso, name: 'Contoso Ltd' };
    mockPatch.mockResolvedValue(ok(renamed));
    useOrganizationStore.setState({ organizations: [contoso, fabrikam] });

    await useOrganizationStore.getState().renameOrganization('org-1', 'Contoso Ltd');

    expect(mockPatch).toHaveBeenCalledWith('/api/v1/organizations/{id}', {
      params: { path: { id: 'org-1' } },
      body: { name: 'Contoso Ltd' },
    });
    expect(useOrganizationStore.getState().organizations).toEqual([renamed, fabrikam]);
  });

  it('drops an archived customer out of the working set', async () => {
    mockPatch.mockResolvedValue(ok({ ...fabrikam, archived_at: '2026-08-06T00:00:00Z' }));
    useOrganizationStore.setState({ organizations: [contoso, fabrikam] });

    await useOrganizationStore.getState().setOrganizationArchived('org-2', true);

    expect(mockPatch).toHaveBeenCalledWith('/api/v1/organizations/{id}', {
      params: { path: { id: 'org-2' } },
      body: { archived: true },
    });
    expect(useOrganizationStore.getState().organizations.map((o) => o.id)).toEqual(['org-1']);
  });

  it('clears the selection when the selected customer is deleted', async () => {
    mockDelete.mockResolvedValue({ response: { ok: true } as Response });
    useOrganizationStore.setState({ organizations: [contoso, fabrikam] });
    useOrganizationStore.getState().selectOrganization('org-2');

    const deleted = await useOrganizationStore.getState().deleteOrganization('org-2');

    expect(deleted).toBe(true);
    expect(useOrganizationStore.getState().organizations).toEqual([contoso]);
    expect(useOrganizationStore.getState().selectedOrganizationId).toBeNull();
  });

  it('keeps the selection when another customer is deleted', async () => {
    mockDelete.mockResolvedValue({ response: { ok: true } as Response });
    useOrganizationStore.setState({ organizations: [contoso, fabrikam] });
    useOrganizationStore.getState().selectOrganization('org-2');

    await useOrganizationStore.getState().deleteOrganization('org-1');

    expect(useOrganizationStore.getState().selectedOrganizationId).toBe('org-2');
  });

  it('keeps the customer when a delete is refused', async () => {
    mockDelete.mockResolvedValue(failed);
    useOrganizationStore.setState({ organizations: [contoso] });

    const deleted = await useOrganizationStore.getState().deleteOrganization('org-1');

    expect(deleted).toBe(false);
    expect(useOrganizationStore.getState().organizations).toEqual([contoso]);
  });

  it('hydrates the remembered selection from storage', () => {
    localStorage.setItem(SELECTED_ORGANIZATION_KEY, 'org-2');
    useOrganizationStore.getState().hydrateSelection();
    expect(useOrganizationStore.getState().selectedOrganizationId).toBe('org-2');
  });

  it('hydrates to no selection when storage is empty', () => {
    useOrganizationStore.getState().hydrateSelection();
    expect(useOrganizationStore.getState().selectedOrganizationId).toBeNull();
  });
});
