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
    const progressBar = document.querySelector('[role="progressbar"]');
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
});
