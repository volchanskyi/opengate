import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useToastStore } from '../../lib/feedback/toast-store';
import { useDeviceStore } from './state/device-store';
import { DEVICE_DRAG_MIME, UNFILED_SITE_ID } from './device-drag';
import { SiteSidebar } from './SiteSidebar';
import { useAuthStore } from '../../state/auth-store';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    POST: vi.fn().mockResolvedValue({ data: { id: 'new', name: 'New' }, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));


function seedAdminUser(isAdmin = true) {
  useAuthStore.setState({
    user: { id: 'u1', email: 'a@b.com', display_name: 'A', is_admin: isAdmin, created_at: '', updated_at: '' },
  });
}

describe('SiteSidebar', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    seedAdminUser();
    useDeviceStore.setState({
      sites: [
        { id: 'g1', organization_id: 'org-1', name: 'Site A', created_at: '', updated_at: '' },
        { id: 'g2', organization_id: 'org-1', name: 'Site B', created_at: '', updated_at: '' },
      ],
      selectedSiteId: 'g1',
      devices: [],
      selectedDevice: null,
      isLoading: false,
      error: null,
    });
  });

  it('renders site names', () => {
    render(<SiteSidebar />);
    expect(screen.getByText('Site A')).toBeInTheDocument();
    expect(screen.getByText('Site B')).toBeInTheDocument();
  });

  it('highlights active site', () => {
    render(<SiteSidebar />);
    const groupA = screen.getByText('Site A').closest('div');
    expect(groupA?.className).toContain('bg-gray-700');
  });

  it('shows create form on + New click', async () => {
    const user = userEvent.setup();
    render(<SiteSidebar />);

    await user.click(screen.getByText('+ New'));
    expect(screen.getByPlaceholderText('Site name')).toBeInTheDocument();
  });

  it('calls selectSite on site click', async () => {
    const user = userEvent.setup();
    const selectGroupFn = vi.fn();
    useDeviceStore.setState({ selectSite: selectGroupFn });

    render(<SiteSidebar />);
    await user.click(screen.getByText('Site B'));

    expect(selectGroupFn).toHaveBeenCalledWith('g2');
  });

  it('delete requires confirmation', async () => {
    const user = userEvent.setup();
    render(<SiteSidebar />);

    const deleteButtons = screen.getAllByText('x');
    await user.click(deleteButtons[0]!);
    expect(screen.getByText('Confirm?')).toBeInTheDocument();
  });

  it('shows empty state', () => {
    useDeviceStore.setState({ sites: [] });
    render(<SiteSidebar />);
    expect(screen.getByText('No sites yet')).toBeInTheDocument();
  });

  it('hides the empty state, and shows the drag hint, once sites exist', () => {
    render(<SiteSidebar />);
    expect(screen.queryByText('No sites yet')).toBeNull();
    expect(screen.getByText(/drag a device card onto a site/i)).toBeInTheDocument();
  });

  it('offers no drop affordances to an admin with no sites', () => {
    useDeviceStore.setState({ sites: [] });
    render(<SiteSidebar />);
    expect(screen.queryByLabelText('Unfiled')).toBeNull();
    expect(screen.queryByText(/drag a device card onto a site/i)).toBeNull();
  });

  it('draws the Unfiled zone as a dashed drop target', () => {
    render(<SiteSidebar />);
    expect(screen.getByLabelText('Unfiled').className).toContain('border-dashed');
  });

  it('clears the pending name so a reopened create form starts empty', async () => {
    const user = userEvent.setup();
    useDeviceStore.setState({ createSite: vi.fn().mockResolvedValue(undefined) });

    render(<SiteSidebar />);
    await user.click(screen.getByText('+ New'));
    await user.type(screen.getByPlaceholderText('Site name'), 'Warehouse');
    await user.click(screen.getByText('Add'));
    await waitFor(() => { expect(screen.queryByPlaceholderText('Site name')).toBeNull(); });

    await user.click(screen.getByText('+ New'));
    expect(screen.getByPlaceholderText<HTMLInputElement>('Site name').value).toBe('');
  });

  it('treats a signed-out viewer as non-admin rather than as an admin', () => {
    useAuthStore.setState({ user: null });
    render(<SiteSidebar />);
    expect(screen.queryByText('+ New')).toBeNull();
  });

  it('calls createSite with trimmed name on form submit, then clears input and hides form', async () => {
    const user = userEvent.setup();
    const createGroupFn = vi.fn().mockResolvedValue(undefined);
    useDeviceStore.setState({ createSite: createGroupFn });

    render(<SiteSidebar />);
    await user.click(screen.getByText('+ New'));

    const input = screen.getByPlaceholderText('Site name') as HTMLInputElement;
    await user.type(input, '  New Site  ');
    await user.click(screen.getByText('Add'));

    expect(createGroupFn).toHaveBeenCalledWith('New Site');
    expect(screen.queryByPlaceholderText('Site name')).toBeNull();
  });

  it('does NOT call createSite when name is whitespace-only', async () => {
    const user = userEvent.setup();
    const createGroupFn = vi.fn();
    useDeviceStore.setState({ createSite: createGroupFn });

    render(<SiteSidebar />);
    await user.click(screen.getByText('+ New'));
    const input = screen.getByPlaceholderText('Site name');
    await user.type(input, '   ');
    await user.click(screen.getByText('Add'));

    expect(createGroupFn).not.toHaveBeenCalled();
  });

  it('first delete click shows Confirm, second click actually deletes', async () => {
    const user = userEvent.setup();
    const deleteGroupFn = vi.fn().mockResolvedValue(undefined);
    useDeviceStore.setState({ deleteSite: deleteGroupFn });

    render(<SiteSidebar />);
    const deleteButtons = screen.getAllByText('x');

    await user.click(deleteButtons[0]!);
    expect(screen.getByText('Confirm?')).toBeInTheDocument();
    expect(deleteGroupFn).not.toHaveBeenCalled();

    await user.click(screen.getByText('Confirm?'));
    expect(deleteGroupFn).toHaveBeenCalledWith('g1');
    await waitFor(() => { expect(screen.queryByText('Confirm?')).not.toBeInTheDocument(); });
  });

  it('adding a site stays on the page rather than submitting it', async () => {
    const user = userEvent.setup();
    render(<SiteSidebar />);
    await user.click(screen.getByText('+ New'));

    const form = screen.getByText('Add').closest('form')!;
    expect(fireEvent.submit(form)).toBe(false);
  });

  it('non-active sites use the gray text style; active uses white-on-gray', () => {
    render(<SiteSidebar />);
    const groupB = screen.getByText('Site B').closest('div');
    expect(groupB?.className).toContain('text-gray-400');
    expect(groupB?.className).not.toContain('bg-gray-700 text-white');
  });

  it('delete button title flips between default and confirm text', async () => {
    const user = userEvent.setup();
    render(<SiteSidebar />);
    const deleteButtons = screen.getAllByText('x');
    expect(deleteButtons[0]!.getAttribute('title')).toBe('Delete site');

    await user.click(deleteButtons[0]!);
    expect(screen.getByText('Confirm?').getAttribute('title')).toBe('Click again to confirm');
  });

  it('clicking another site delete button moves confirmation focus (only one Confirm? rendered)', async () => {
    const user = userEvent.setup();
    render(<SiteSidebar />);
    const deleteButtons = screen.getAllByText('x');
    await user.click(deleteButtons[0]!);
    expect(screen.getByText('Confirm?')).toBeInTheDocument();
    await user.click(screen.getByText('x'));
    expect(screen.getAllByText('Confirm?')).toHaveLength(1);
  });

  it('+ New button toggles label to Cancel when the form is open', async () => {
    const user = userEvent.setup();
    render(<SiteSidebar />);
    await user.click(screen.getByText('+ New'));
    expect(screen.getByText('Cancel')).toBeInTheDocument();
    expect(screen.queryByText('+ New')).toBeNull();

    await user.click(screen.getByText('Cancel'));
    expect(screen.getByText('+ New')).toBeInTheDocument();
    expect(screen.queryByText('Cancel')).toBeNull();
  });

  it('Sites heading is rendered as a heading element', () => {
    render(<SiteSidebar />);
    const heading = screen.getByRole('heading', { name: 'Sites' });
    expect(heading.tagName).toBe('H2');
  });

  describe('drop target for device drags', () => {
    const device = {
      id: 'd1', organization_id: 'org-1', site_id: 'g1', hostname: 'web-01', os: 'linux', agent_version: '1.0.0',
      capabilities: [], status: 'online' as const, last_seen: '', created_at: '', updated_at: '',
    };

    const deviceTransfer = (id = 'd1') => ({
      types: [DEVICE_DRAG_MIME],
      getData: (type: string) => (type === DEVICE_DRAG_MIME ? id : ''),
      dropEffect: 'none',
    });

    const dropZone = (name: string) => screen.getByRole('listitem', { name });

    beforeEach(() => {
      useDeviceStore.setState({ devices: [device] });
    });

    it('dropping a device on a site moves it there', async () => {
      const updateDeviceSite = vi.fn().mockResolvedValue(true);
      const fetchDevices = vi.fn().mockResolvedValue(undefined);
      useDeviceStore.setState({ updateDeviceSite, fetchDevices });
      render(<SiteSidebar />);

      fireEvent.drop(dropZone('Site B'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(updateDeviceSite).toHaveBeenCalledWith('d1', 'g2'); });
      await waitFor(() => { expect(fetchDevices).toHaveBeenCalledWith('g1'); });
    });

    it('dropping on the Unfiled zone clears the device site', async () => {
      const updateDeviceSite = vi.fn().mockResolvedValue(true);
      useDeviceStore.setState({ updateDeviceSite, fetchDevices: vi.fn() });
      render(<SiteSidebar />);

      fireEvent.drop(dropZone('Unfiled'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(updateDeviceSite).toHaveBeenCalledWith('d1', UNFILED_SITE_ID); });
    });

    it('names the device and the destination in the success toast', async () => {
      const addToast = vi.fn();
      useToastStore.setState({ addToast });
      useDeviceStore.setState({ updateDeviceSite: vi.fn().mockResolvedValue(true), fetchDevices: vi.fn() });
      render(<SiteSidebar />);

      fireEvent.drop(dropZone('Site B'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(addToast).toHaveBeenCalledWith('Moved web-01 to Site B', 'success'); });
    });

    it('reports a failed move and does not re-pull the list', async () => {
      const addToast = vi.fn();
      const fetchDevices = vi.fn();
      useToastStore.setState({ addToast });
      useDeviceStore.setState({ updateDeviceSite: vi.fn().mockResolvedValue(false), fetchDevices });
      render(<SiteSidebar />);

      fireEvent.drop(dropZone('Site B'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(addToast).toHaveBeenCalledWith('Failed to move web-01 to Site B', 'error'); });
      expect(fetchDevices).not.toHaveBeenCalled();
    });

    it('dropping a device on the site it is already in is a no-op', async () => {
      const updateDeviceSite = vi.fn();
      useDeviceStore.setState({ updateDeviceSite, fetchDevices: vi.fn() });
      render(<SiteSidebar />);

      fireEvent.drop(dropZone('Site A'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(updateDeviceSite).not.toHaveBeenCalled(); });
    });

    it('moves the dragged device, not the first one in the list', async () => {
      const other = { ...device, id: 'd0', organization_id: 'org-1', site_id: 'g2', hostname: 'db-01' };
      const updateDeviceSite = vi.fn().mockResolvedValue(true);
      const addToast = vi.fn();
      useToastStore.setState({ addToast });
      useDeviceStore.setState({ devices: [other, device], updateDeviceSite, fetchDevices: vi.fn() });
      render(<SiteSidebar />);

      fireEvent.drop(dropZone('Site B'), { dataTransfer: deviceTransfer('d1') });

      await waitFor(() => { expect(updateDeviceSite).toHaveBeenCalledWith('d1', 'g2'); });
      expect(addToast).toHaveBeenCalledWith('Moved web-01 to Site B', 'success');
    });

    it.each([
      ['an empty site_id', ''],
      ['a whitespace site_id', '   '],
      ['the placeholder site_id', UNFILED_SITE_ID],
    ])('dropping a device with %s onto Unfiled is a no-op', async (_label, siteId) => {
      const updateDeviceSite = vi.fn();
      useDeviceStore.setState({
        devices: [{ ...device, organization_id: 'org-1', site_id: siteId }],
        updateDeviceSite,
        fetchDevices: vi.fn(),
      });
      render(<SiteSidebar />);

      fireEvent.drop(dropZone('Unfiled'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(updateDeviceSite).not.toHaveBeenCalled(); });
    });

    it('a device dragged over a site is accepted there, and its drop is taken', () => {
      useDeviceStore.setState({ updateDeviceSite: vi.fn().mockResolvedValue(true), fetchDevices: vi.fn() });
      render(<SiteSidebar />);

      expect(fireEvent.dragOver(dropZone('Site B'), { dataTransfer: deviceTransfer() })).toBe(false);
      expect(fireEvent.drop(dropZone('Site B'), { dataTransfer: deviceTransfer() })).toBe(false);
    });

    it('dropping clears the highlight', () => {
      useDeviceStore.setState({ updateDeviceSite: vi.fn().mockResolvedValue(true), fetchDevices: vi.fn() });
      render(<SiteSidebar />);

      fireEvent.dragOver(dropZone('Site B'), { dataTransfer: deviceTransfer() });
      expect(dropZone('Site B')).toHaveClass('ring-2');
      fireEvent.drop(dropZone('Site B'), { dataTransfer: deviceTransfer() });

      expect(dropZone('Site B')).not.toHaveClass('ring-2');
    });

    it('leaving one zone does not clear the highlight on the zone now hovered', () => {
      useDeviceStore.setState({ updateDeviceSite: vi.fn(), fetchDevices: vi.fn() });
      render(<SiteSidebar />);

      fireEvent.dragOver(dropZone('Site A'), { dataTransfer: deviceTransfer() });
      fireEvent.dragOver(dropZone('Site B'), { dataTransfer: deviceTransfer() });
      fireEvent.dragLeave(dropZone('Site A'));

      expect(dropZone('Site B')).toHaveClass('ring-2');
      expect(dropZone('Site A')).not.toHaveClass('ring-2');
    });

    it('ignores a drop that carries no device', async () => {
      const updateDeviceSite = vi.fn();
      useDeviceStore.setState({ updateDeviceSite, fetchDevices: vi.fn() });
      render(<SiteSidebar />);

      fireEvent.drop(dropZone('Site B'), {
        dataTransfer: {
          types: ['text/plain'],
          getData: (type: string) => (type === 'text/plain' ? 'hello' : ''),
          dropEffect: 'none',
        },
      });

      await waitFor(() => { expect(updateDeviceSite).not.toHaveBeenCalled(); });
    });

    it('highlights only the hovered zone while a device drag is over it', () => {
      useDeviceStore.setState({ updateDeviceSite: vi.fn(), fetchDevices: vi.fn() });
      render(<SiteSidebar />);
      const target = dropZone('Site B');

      fireEvent.dragOver(target, { dataTransfer: deviceTransfer() });
      expect(target).toHaveClass('ring-2');
      expect(dropZone('Site A')).not.toHaveClass('ring-2');

      fireEvent.dragLeave(target);
      expect(target).not.toHaveClass('ring-2');
    });

    it('does not highlight for a drag that carries no device', () => {
      render(<SiteSidebar />);
      const target = dropZone('Site B');

      fireEvent.dragOver(target, { dataTransfer: { types: ['text/plain'], getData: () => '', dropEffect: 'none' } });

      expect(target).not.toHaveClass('ring-2');
    });
  });

  describe('non-admin', () => {
    beforeEach(() => { seedAdminUser(false); });

    it('omits the create-site control from the DOM', () => {
      render(<SiteSidebar />);
      expect(screen.queryByText('+ New')).toBeNull();
    });

    it('omits every delete-site control from the DOM', () => {
      render(<SiteSidebar />);
      expect(screen.queryByTitle('Delete site')).toBeNull();
    });

    it('omits the drag-to-move affordances', () => {
      render(<SiteSidebar />);
      expect(screen.queryByText(/drag a device card onto a site/i)).toBeNull();
      expect(screen.queryByLabelText('Unfiled')).toBeNull();
    });

    it('still exposes each site as a labelled list item', () => {
      render(<SiteSidebar />);
      expect(screen.getByRole('listitem', { name: 'Site A' })).toBeInTheDocument();
      expect(screen.getByRole('listitem', { name: 'Site B' })).toBeInTheDocument();
    });

    it('still lists and selects sites', async () => {
      const user = userEvent.setup();
      const selectGroupFn = vi.fn();
      useDeviceStore.setState({ selectSite: selectGroupFn });
      render(<SiteSidebar />);
      expect(screen.getByText('Site A')).toBeInTheDocument();
      await user.click(screen.getByText('Site B'));
      expect(selectGroupFn).toHaveBeenCalledWith('g2');
    });
  });
});
