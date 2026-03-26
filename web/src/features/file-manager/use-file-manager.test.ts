import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useConnectionStore } from '../../state/connection-store';
import { useFileStore } from '../../state/file-store';
import { useFileManager } from './use-file-manager';

const originalCreateElement = document.createElement.bind(document);

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

describe('useFileManager', () => {
  const mockSendControl = vi.fn();
  let capturedFileFrameHandler: ((frame: { offset: number; total_size: number; data: Uint8Array }) => void) | null = null;

  afterEach(() => {
    vi.restoreAllMocks();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    capturedFileFrameHandler = null;

    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
      setOnControlMessage: vi.fn(),
      setOnFileFrame: vi.fn((cb) => {
        capturedFileFrameHandler = cb;
      }),
    });

    useFileStore.setState({
      currentPath: '/',
      entries: [],
      isLoading: false,
      error: null,
      downloads: {},
      uploads: {},
      viewingFile: null,
    });
  });

  it('subscribes to file frames when transport exists', () => {
    renderHook(() => useFileManager());
    expect(useConnectionStore.getState().setOnFileFrame).toHaveBeenCalled();
    expect(capturedFileFrameHandler).toBeInstanceOf(Function);
  });

  it('unsubscribes on unmount', () => {
    const { unmount } = renderHook(() => useFileManager());
    unmount();
    // setOnFileFrame should have been called with null on cleanup
    const calls = (useConnectionStore.getState().setOnFileFrame as ReturnType<typeof vi.fn>).mock.calls;
    expect(calls[calls.length - 1]![0]).toBeNull();
  });

  it('requestDownload sends FileDownloadRequest and sets initial progress', () => {
    const { result } = renderHook(() => useFileManager());

    act(() => {
      result.current.requestDownload('/home/test.txt');
    });

    expect(mockSendControl).toHaveBeenCalledWith({
      type: 'FileDownloadRequest',
      path: '/home/test.txt',
    });
    expect(useFileStore.getState().downloads['test.txt']).toBe(0);
  });

  it('requestView sends FileDownloadRequest and sets initial progress', () => {
    const { result } = renderHook(() => useFileManager());

    act(() => {
      result.current.requestView('/home/readme.md');
    });

    expect(mockSendControl).toHaveBeenCalledWith({
      type: 'FileDownloadRequest',
      path: '/home/readme.md',
    });
    expect(useFileStore.getState().downloads['readme.md']).toBe(0);
  });

  it('accumulates file frames and updates download progress', () => {
    const { result } = renderHook(() => useFileManager());

    act(() => {
      result.current.requestDownload('/home/file.bin');
    });

    act(() => {
      capturedFileFrameHandler!({ offset: 0, total_size: 10, data: new Uint8Array(5) });
    });

    expect(useFileStore.getState().downloads['file.bin']).toBeCloseTo(0.5);
  });

  it('triggers browser save on completed download', () => {
    const createObjectURL = vi.fn(() => 'blob:mock-url');
    const revokeObjectURL = vi.fn();
    globalThis.URL.createObjectURL = createObjectURL;
    globalThis.URL.revokeObjectURL = revokeObjectURL;

    const mockClick = vi.fn();
    const mockAnchor = { href: '', download: '', click: mockClick } as unknown as HTMLAnchorElement;

    // Render hook first, then mock DOM methods (so React can mount)
    const { result } = renderHook(() => useFileManager());

    vi.spyOn(document, 'createElement').mockImplementation((tag: string) =>
      tag === 'a' ? (mockAnchor as never) : originalCreateElement(tag),
    );
    vi.spyOn(document.body, 'appendChild').mockImplementation(() => mockAnchor as never);
    vi.spyOn(document.body, 'removeChild').mockImplementation(() => mockAnchor as never);

    act(() => {
      result.current.requestDownload('/home/file.bin');
    });

    act(() => {
      capturedFileFrameHandler!({ offset: 0, total_size: 5, data: new Uint8Array([1, 2, 3, 4, 5]) });
    });

    expect(createObjectURL).toHaveBeenCalled();
    expect(mockAnchor.download).toBe('file.bin');
    expect(mockClick).toHaveBeenCalled();
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock-url');
    expect(useFileStore.getState().downloads['file.bin']).toBeUndefined();
  });

  it('sets viewingFile on completed view request', async () => {
    const { result } = renderHook(() => useFileManager());

    act(() => {
      result.current.requestView('/home/hello.txt');
    });

    const textEncoder = new TextEncoder();
    const data = textEncoder.encode('hello world');

    act(() => {
      capturedFileFrameHandler!({ offset: 0, total_size: data.length, data });
    });

    // Blob.text() is async, wait for state update
    await vi.waitFor(() => {
      expect(useFileStore.getState().viewingFile).not.toBeNull();
    });

    expect(useFileStore.getState().viewingFile?.name).toBe('hello.txt');
    expect(useFileStore.getState().viewingFile?.content).toBe('hello world');
    expect(useFileStore.getState().downloads['hello.txt']).toBeUndefined();
  });

  it('handles empty file (total_size=0) download', () => {
    const createObjectURL = vi.fn(() => 'blob:empty');
    const revokeObjectURL = vi.fn();
    globalThis.URL.createObjectURL = createObjectURL;
    globalThis.URL.revokeObjectURL = revokeObjectURL;

    const mockClick = vi.fn();
    const mockAnchor = { href: '', download: '', click: mockClick } as unknown as HTMLAnchorElement;

    const { result } = renderHook(() => useFileManager());

    vi.spyOn(document, 'createElement').mockImplementation((tag: string) =>
      tag === 'a' ? (mockAnchor as never) : originalCreateElement(tag),
    );
    vi.spyOn(document.body, 'appendChild').mockImplementation(() => mockAnchor as never);
    vi.spyOn(document.body, 'removeChild').mockImplementation(() => mockAnchor as never);

    act(() => {
      result.current.requestDownload('/home/empty.txt');
    });

    act(() => {
      capturedFileFrameHandler!({ offset: 0, total_size: 0, data: new Uint8Array(0) });
    });

    expect(createObjectURL).toHaveBeenCalled();
    expect(mockClick).toHaveBeenCalled();
    expect(useFileStore.getState().downloads['empty.txt']).toBeUndefined();
  });
});
