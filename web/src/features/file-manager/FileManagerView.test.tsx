import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useConnectionStore } from '../../state/connection-store';
import { useFileStore } from '../../state/file-store';
import { FileManagerView } from './FileManagerView';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

describe('FileManagerView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useConnectionStore.setState({
      state: 'connected',
      transport: null,
    });
    useFileStore.setState({
      currentPath: '/home',
      entries: [
        { name: 'docs', is_dir: true, size: 0, modified: 1000 },
        { name: 'file.txt', is_dir: false, size: 1024, modified: 2000 },
      ],
      isLoading: false,
      error: null,
      downloads: {},
      uploads: {},
      viewingFile: null,
    });
  });

  it('renders directory listing', () => {
    render(<FileManagerView />);
    expect(screen.getByText('docs')).toBeInTheDocument();
    expect(screen.getByText('file.txt')).toBeInTheDocument();
  });

  it('renders breadcrumb with current path', () => {
    render(<FileManagerView />);
    expect(screen.getByText('/home')).toBeInTheDocument();
  });

  it('shows folder icon for directories', () => {
    render(<FileManagerView />);
    const docsRow = screen.getByText('docs').closest('tr');
    expect(docsRow?.textContent).toContain('docs');
  });

  it('shows placeholder when disconnected', () => {
    useConnectionStore.setState({ state: 'disconnected' });
    render(<FileManagerView />);
    expect(screen.getByText(/waiting for connection/i)).toBeInTheDocument();
  });

  it('shows download progress bar', () => {
    useFileStore.setState({ downloads: { 'file.txt': 0.5 } });
    render(<FileManagerView />);
    const progressBar = document.querySelector('progress');
    expect(progressBar).toBeInTheDocument();
  });

  it('clicking directory name navigates into it', async () => {
    const user = userEvent.setup();
    const mockSendControl = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
    });

    render(<FileManagerView />);
    await user.click(screen.getByText('docs'));

    expect(mockSendControl).toHaveBeenCalledWith({
      type: 'FileListRequest',
      path: '/home/docs',
    });
  });

  it('shows View and Download buttons for files but not directories', () => {
    render(<FileManagerView />);
    // File entry should have both buttons
    expect(screen.getByLabelText('View file.txt')).toBeInTheDocument();
    expect(screen.getByLabelText('Download file.txt')).toBeInTheDocument();
    // Directory entry should not have buttons
    expect(screen.queryByLabelText('View docs')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Download docs')).not.toBeInTheDocument();
  });

  it('clicking Download button sends FileDownloadRequest with full path', async () => {
    const user = userEvent.setup();
    const mockSendControl = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
    });

    render(<FileManagerView />);
    await user.click(screen.getByLabelText('Download file.txt'));

    expect(mockSendControl).toHaveBeenCalledWith({
      type: 'FileDownloadRequest',
      path: '/home/file.txt',
    });
  });

  it('clicking View button sends FileDownloadRequest with full path', async () => {
    const user = userEvent.setup();
    const mockSendControl = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
    });

    render(<FileManagerView />);
    await user.click(screen.getByLabelText('View file.txt'));

    expect(mockSendControl).toHaveBeenCalledWith({
      type: 'FileDownloadRequest',
      path: '/home/file.txt',
    });
  });

  it('disables buttons while download is in progress', () => {
    useFileStore.setState({ downloads: { 'file.txt': 0.5 } });
    render(<FileManagerView />);
    expect(screen.getByLabelText('View file.txt')).toBeDisabled();
    expect(screen.getByLabelText('Download file.txt')).toBeDisabled();
  });

  it('renders file viewer when viewingFile is set', () => {
    useFileStore.setState({
      viewingFile: { name: 'readme.md', content: '# Hello World' },
    });
    render(<FileManagerView />);
    expect(screen.getByText('readme.md')).toBeInTheDocument();
    expect(screen.getByText('# Hello World')).toBeInTheDocument();
  });

  it('close button on viewer clears viewingFile', async () => {
    const user = userEvent.setup();
    useFileStore.setState({
      viewingFile: { name: 'readme.md', content: '# Hello' },
    });
    render(<FileManagerView />);

    await user.click(screen.getByLabelText('Close viewer'));
    expect(useFileStore.getState().viewingFile).toBeNull();
  });

  it('sends a one-shot FileListRequest for / when connection becomes connected', () => {
    const mockSendControl = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
    });
    useFileStore.setState({ currentPath: '/home', entries: [] });

    const { rerender } = render(<FileManagerView />);
    // Initial mount fires one FileListRequest with path "/".
    expect(mockSendControl).toHaveBeenCalledWith({ type: 'FileListRequest', path: '/' });
    expect(mockSendControl).toHaveBeenCalledTimes(1);

    // Re-rendering with the same connected state must NOT fire a second request.
    rerender(<FileManagerView />);
    expect(mockSendControl).toHaveBeenCalledTimes(1);
  });

  it('does not send an initial FileListRequest while disconnected', () => {
    const mockSendControl = vi.fn();
    useConnectionStore.setState({
      state: 'disconnected',
      transport: { sendControl: mockSendControl } as never,
    });

    render(<FileManagerView />);
    expect(mockSendControl).not.toHaveBeenCalled();
  });

  it('Up button is hidden when currentPath is /', () => {
    useFileStore.setState({ currentPath: '/', entries: [] });
    render(<FileManagerView />);
    // The ".." nav button is omitted at root.
    expect(screen.queryByRole('button', { name: '..' })).not.toBeInTheDocument();
  });

  it('Up button is rendered when currentPath is not /', () => {
    useFileStore.setState({ currentPath: '/home', entries: [] });
    render(<FileManagerView />);
    expect(screen.getByRole('button', { name: '..' })).toBeInTheDocument();
  });

  it('navigateUp from /home goes to /', async () => {
    const user = userEvent.setup();
    const mockSendControl = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
    });
    useFileStore.setState({ currentPath: '/home', entries: [] });

    render(<FileManagerView />);
    mockSendControl.mockClear();
    await user.click(screen.getByRole('button', { name: '..' }));
    expect(mockSendControl).toHaveBeenCalledWith({ type: 'FileListRequest', path: '/' });
  });

  it('navigateUp from a multi-segment path strips the last segment', async () => {
    const user = userEvent.setup();
    const mockSendControl = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
    });
    useFileStore.setState({ currentPath: '/a/b/c', entries: [] });

    render(<FileManagerView />);
    mockSendControl.mockClear();
    await user.click(screen.getByRole('button', { name: '..' }));
    expect(mockSendControl).toHaveBeenCalledWith({ type: 'FileListRequest', path: '/a/b' });
  });

  it('navigateToDir from / builds /<name> (not //<name>)', async () => {
    const user = userEvent.setup();
    const mockSendControl = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
    });
    useFileStore.setState({
      currentPath: '/',
      entries: [{ name: 'docs', is_dir: true, size: 0, modified: 1000 }],
    });

    render(<FileManagerView />);
    mockSendControl.mockClear();
    await user.click(screen.getByText('docs'));
    expect(mockSendControl).toHaveBeenCalledWith({ type: 'FileListRequest', path: '/docs' });
  });

  it('View button at root uses /<name> path (no leading //)', async () => {
    const user = userEvent.setup();
    const mockSendControl = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
    });
    useFileStore.setState({
      currentPath: '/',
      entries: [{ name: 'readme.md', is_dir: false, size: 100, modified: 1000 }],
    });

    render(<FileManagerView />);
    mockSendControl.mockClear();
    await user.click(screen.getByLabelText('View readme.md'));
    expect(mockSendControl).toHaveBeenCalledWith({ type: 'FileDownloadRequest', path: '/readme.md' });
  });

  it('shows error banner when fileError is set', () => {
    useFileStore.setState({ error: 'permission denied' });
    render(<FileManagerView />);
    expect(screen.getByText('permission denied')).toBeInTheDocument();
  });

  it('omits error banner when fileError is null', () => {
    useFileStore.setState({ error: null });
    render(<FileManagerView />);
    expect(document.querySelector('.bg-red-900\\/50')).toBeNull();
  });

  it('formatSize: under 1024 bytes shows raw byte count with "B" suffix', () => {
    useFileStore.setState({
      entries: [{ name: 'tiny', is_dir: false, size: 512, modified: 1000 }],
    });
    render(<FileManagerView />);
    const sizeCell = screen.getByText('tiny').closest('tr')!.querySelectorAll('td')[1];
    expect(sizeCell?.textContent).toBe('512 B');
  });

  it('formatSize: exactly 1024 bytes is the KB boundary → "1.0 KB"', () => {
    useFileStore.setState({
      entries: [{ name: 'k', is_dir: false, size: 1024, modified: 1000 }],
    });
    render(<FileManagerView />);
    const sizeCell = screen.getByText('k').closest('tr')!.querySelectorAll('td')[1];
    expect(sizeCell?.textContent).toBe('1.0 KB');
  });

  it('formatSize: kilobyte range with non-trivial value', () => {
    useFileStore.setState({
      entries: [{ name: 'k', is_dir: false, size: 4096, modified: 1000 }],
    });
    render(<FileManagerView />);
    const sizeCell = screen.getByText('k').closest('tr')!.querySelectorAll('td')[1];
    expect(sizeCell?.textContent).toBe('4.0 KB');
  });

  it('formatSize: exactly 1 MB → "1.0 MB"', () => {
    useFileStore.setState({
      entries: [{ name: 'm', is_dir: false, size: 1024 * 1024, modified: 1000 }],
    });
    render(<FileManagerView />);
    const sizeCell = screen.getByText('m').closest('tr')!.querySelectorAll('td')[1];
    expect(sizeCell?.textContent).toBe('1.0 MB');
  });

  it('formatSize: exactly 1 GB → "1.0 GB"', () => {
    useFileStore.setState({
      entries: [{ name: 'g', is_dir: false, size: 1024 * 1024 * 1024, modified: 1000 }],
    });
    render(<FileManagerView />);
    const sizeCell = screen.getByText('g').closest('tr')!.querySelectorAll('td')[1];
    expect(sizeCell?.textContent).toBe('1.0 GB');
  });

  it('directory rows render "-" instead of formatSize result', () => {
    useFileStore.setState({
      entries: [{ name: 'dirA', is_dir: true, size: 9999, modified: 1000 }],
    });
    render(<FileManagerView />);
    const sizeCell = screen.getByText('dirA').closest('tr')!.querySelectorAll('td')[1];
    expect(sizeCell?.textContent).toBe('-');
  });

  it('Modified column converts unix seconds to a locale date string', () => {
    // 1700000000 seconds = 2023-11-14 ish UTC. Locale specific; just ensure it is a real date.
    useFileStore.setState({
      entries: [{ name: 'dated', is_dir: false, size: 1, modified: 1700000000 }],
    });
    render(<FileManagerView />);
    const modCell = screen.getByText('dated').closest('tr')!.querySelectorAll('td')[2];
    const expected = new Date(1700000000 * 1000).toLocaleString();
    expect(modCell?.textContent).toBe(expected);
  });

  it('download progress value is rounded percentage of the [0,1] fraction', () => {
    useFileStore.setState({ downloads: { 'file.txt': 0.5 } });
    render(<FileManagerView />);
    const progress = document.querySelector('progress') as HTMLProgressElement;
    expect(progress).toBeTruthy();
    expect(progress.getAttribute('value')).toBe('50');
    expect(progress.getAttribute('max')).toBe('100');
  });
});
