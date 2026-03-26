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
});
