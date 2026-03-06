import { describe, it, expect, beforeEach } from 'vitest';
import { useFileStore } from './file-store';

describe('file-store', () => {
  beforeEach(() => {
    useFileStore.setState({
      currentPath: '/',
      entries: [],
      isLoading: false,
      error: null,
      downloads: {},
      uploads: {},
    });
  });

  it('has correct initial state', () => {
    const state = useFileStore.getState();
    expect(state.currentPath).toBe('/');
    expect(state.entries).toEqual([]);
    expect(state.isLoading).toBe(false);
  });

  it('setEntries updates entries and path', () => {
    const { setEntries } = useFileStore.getState();
    const entries = [
      { name: 'docs', is_dir: true, size: 0, modified: 1000 },
      { name: 'file.txt', is_dir: false, size: 42, modified: 2000 },
    ];
    setEntries('/home', entries);

    const state = useFileStore.getState();
    expect(state.currentPath).toBe('/home');
    expect(state.entries).toEqual(entries);
  });

  it('setDownloadProgress tracks download progress', () => {
    const { setDownloadProgress } = useFileStore.getState();
    setDownloadProgress('file.txt', 0.5);
    expect(useFileStore.getState().downloads['file.txt']).toBe(0.5);

    setDownloadProgress('file.txt', 1.0);
    expect(useFileStore.getState().downloads['file.txt']).toBe(1.0);
  });

  it('clearDownload removes download entry', () => {
    const { setDownloadProgress, clearDownload } = useFileStore.getState();
    setDownloadProgress('file.txt', 0.5);
    clearDownload('file.txt');
    expect(useFileStore.getState().downloads['file.txt']).toBeUndefined();
  });

  it('setUploadProgress tracks upload progress', () => {
    const { setUploadProgress } = useFileStore.getState();
    setUploadProgress('upload.bin', 0.75);
    expect(useFileStore.getState().uploads['upload.bin']).toBe(0.75);
  });

  it('clearUpload removes upload entry', () => {
    const { setUploadProgress, clearUpload } = useFileStore.getState();
    setUploadProgress('upload.bin', 0.5);
    clearUpload('upload.bin');
    expect(useFileStore.getState().uploads['upload.bin']).toBeUndefined();
  });

  it('setError sets error state', () => {
    const { setError } = useFileStore.getState();
    setError('something went wrong');
    expect(useFileStore.getState().error).toBe('something went wrong');
  });

  it('setLoading sets loading state', () => {
    const { setLoading } = useFileStore.getState();
    setLoading(true);
    expect(useFileStore.getState().isLoading).toBe(true);
    setLoading(false);
    expect(useFileStore.getState().isLoading).toBe(false);
  });
});
