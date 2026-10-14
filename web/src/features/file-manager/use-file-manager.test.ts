import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useConnectionStore } from '../session';
import { useFileStore } from './state/file-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { BUFFERED_LIMIT } from './download-saver';
import { useFileManager, VIEW_LIMIT } from './use-file-manager';
import type { ControlMessage, FileEntry, FileFrame } from '../../lib/protocol/types';

const entry = (name: string): FileEntry => ({ name, is_dir: false, size: 1, modified: 0 });

describe('useFileManager', () => {
  const sendControl = vi.fn();
  const stopControl = vi.fn();
  let onFrame: ((frame: FileFrame) => void) | null = null;
  let onControl: ((msg: ControlMessage) => void) | null = null;
  const postMessage = vi.fn();

  function frame(offset: number, total: number, text: string) {
    act(() => onFrame!({ offset, total_size: total, data: new TextEncoder().encode(text) }));
  }

  function control(msg: ControlMessage) {
    act(() => onControl!(msg));
  }

  function workerMessages(): string[] {
    return postMessage.mock.calls.map((c) => (c[0] as { type: string }).type).filter((t) => t !== 'keepalive');
  }

  beforeEach(() => {
    stopControl.mockReset();
    sendControl.mockReset();
    postMessage.mockReset();
    onFrame = null;
    onControl = null;
    vi.stubGlobal('navigator', { ...navigator, serviceWorker: { controller: { postMessage } } });
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl } as never,
      listenControl: vi.fn((cb) => {
        onControl = cb;
        return stopControl;
      }),
      setOnFileFrame: vi.fn((cb) => {
        onFrame = cb;
      }),
    });
    useFileStore.setState({
      currentPath: '/',
      entries: [],
      isLoading: false,
      error: null,
      downloads: {},
      waiting: [],
      uploads: {},
      viewingFile: null,
    });
    useToastStore.setState({ toasts: [] });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    document.body.innerHTML = '';
  });

  it('stops taking frames and messages on unmount', () => {
    const { unmount } = renderHook(() => useFileManager());
    unmount();
    const frames = (useConnectionStore.getState().setOnFileFrame as ReturnType<typeof vi.fn>).mock.calls;
    expect(frames.at(-1)![0]).toBeNull();
    expect(stopControl).toHaveBeenCalledOnce();
  });

  describe('listings', () => {
    it('fills a folder in as its pages arrive, and stops loading at the last', () => {
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestDirectory('/var/log'));
      expect(sendControl).toHaveBeenCalledWith({ type: 'FileListRequest', path: '/var/log' });

      control({ type: 'FileListResponse', path: '/var/log', entries: [entry('a.log')], more: true });
      expect(useFileStore.getState().isLoading).toBe(true);
      control({ type: 'FileListResponse', path: '/var/log', entries: [entry('b.log')] });

      expect(useFileStore.getState().entries.map((e) => e.name)).toEqual(['a.log', 'b.log']);
      expect(useFileStore.getState().currentPath).toBe('/var/log');
      expect(useFileStore.getState().isLoading).toBe(false);
    });

    it('drops pages of a folder the technician has left', () => {
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestDirectory('/var/log'));
      act(() => result.current.requestDirectory('/etc'));
      control({ type: 'FileListResponse', path: '/var/log', entries: [entry('a.log')], more: true });
      control({ type: 'FileListResponse', path: '/etc', entries: [entry('hosts')] });
      expect(useFileStore.getState().entries.map((e) => e.name)).toEqual(['hosts']);
    });

    it('shows a listing the host refused as an error', () => {
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestDirectory('/root'));
      control({ type: 'FileListError', path: '/root', error: 'permission denied' });
      expect(useFileStore.getState().error).toBe('permission denied');
      expect(useFileStore.getState().isLoading).toBe(false);
    });

    it('clears the last folder’s error when another opens', () => {
      useFileStore.setState({ error: 'permission denied' });
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestDirectory('/home'));
      expect(useFileStore.getState().error).toBeNull();
    });
  });

  describe('downloads', () => {
    it('streams a download through the service worker as its pieces arrive', () => {
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestDownload('/var/log/a.log'));
      expect(sendControl).toHaveBeenCalledWith({ type: 'FileDownloadRequest', path: '/var/log/a.log' });
      expect(useFileStore.getState().downloads['a.log']).toBe(0);

      frame(0, 10, 'hello');
      expect(useFileStore.getState().downloads['a.log']).toBe(0.5);
      frame(5, 10, 'world');

      expect(workerMessages()).toEqual(['download-start', 'download-chunk', 'download-chunk', 'download-end']);
      expect(useFileStore.getState().downloads['a.log']).toBeUndefined();
    });

    it('runs one download at a time, the next waiting until the first is saved', () => {
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestDownload('/var/log/a.log'));
      act(() => result.current.requestView('/var/log/b.log'));

      expect(sendControl).toHaveBeenCalledTimes(1);
      expect(useFileStore.getState().waiting).toEqual(['b.log']);

      frame(0, 3, 'abc');
      expect(sendControl).toHaveBeenLastCalledWith({ type: 'FileDownloadRequest', path: '/var/log/b.log' });
      expect(useFileStore.getState().waiting).toEqual([]);

      frame(0, 2, 'ok');
      expect(useFileStore.getState().viewingFile).toEqual({ name: 'b.log', content: 'ok' });
    });

    it('ends a download the host could not read with its reason, and moves to the next', () => {
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestDownload('/var/log/a.log'));
      act(() => result.current.requestDownload('/var/log/b.log'));
      frame(0, 10, 'half!');

      control({ type: 'FileDownloadError', path: '/var/log/a.log', error: 'input/output error' });

      expect(workerMessages()).toContain('download-abort');
      expect(useToastStore.getState().toasts.at(-1)!.message).toBe("Couldn't download a.log: input/output error");
      expect(useFileStore.getState().downloads['a.log']).toBeUndefined();
      expect(sendControl).toHaveBeenLastCalledWith({ type: 'FileDownloadRequest', path: '/var/log/b.log' });
    });

    it('says a file is past what this browser can save without a service worker, and holds none of it', () => {
      vi.stubGlobal('navigator', { ...navigator, serviceWorker: { controller: null } });
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestDownload('/srv/disk.vhdx'));
      act(() => result.current.requestDownload('/srv/notes.txt'));

      frame(0, BUFFERED_LIMIT + 1, 'x');
      expect(useToastStore.getState().toasts.at(-1)!.message).toMatch(/up to 64 MiB/);
      expect(sendControl).toHaveBeenCalledTimes(1);
    });
  });

  describe('views', () => {
    it('shows a small file’s text', () => {
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestView('/etc/hostname'));
      frame(0, 5, 'fs01\n');
      expect(useFileStore.getState().viewingFile).toEqual({ name: 'hostname', content: 'fs01\n' });
    });

    it('declines to show a file past the view limit, and suggests downloading it', () => {
      const { result } = renderHook(() => useFileManager());
      act(() => result.current.requestView('/var/log/huge.log'));
      frame(0, VIEW_LIMIT + 1, 'x');
      expect(useToastStore.getState().toasts.at(-1)!.message).toBe('huge.log is too large to view; download it instead');
      expect(useFileStore.getState().viewingFile).toBeNull();
    });
  });
});
