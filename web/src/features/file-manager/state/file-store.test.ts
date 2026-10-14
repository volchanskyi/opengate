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
      waiting: [],
    });
  });

  it('setEntries updates entries and path AND clears isLoading', () => {
    // Seed isLoading true to prove the call resets it. Kills BooleanLiteral
    // mutant on `isLoading: false` inside setEntries.
    useFileStore.setState({ isLoading: true });
    const { setEntries } = useFileStore.getState();
    const entries = [
      { name: 'docs', is_dir: true, size: 0, modified: 1000 },
      { name: 'file.txt', is_dir: false, size: 42, modified: 2000 },
    ];
    setEntries('/home', entries);

    const state = useFileStore.getState();
    expect(state.currentPath).toBe('/home');
    expect(state.entries).toEqual(entries);
    expect(state.isLoading).toBe(false);
  });

  it('keeps loading while a listing has pages to come, and appends each one', () => {
    const { setEntries, appendEntries } = useFileStore.getState();
    setEntries('/var/log', [{ name: 'a.log', is_dir: false, size: 1, modified: 0 }], true);
    expect(useFileStore.getState().isLoading).toBe(true);

    appendEntries([{ name: 'b.log', is_dir: false, size: 2, modified: 0 }], true);
    appendEntries([{ name: 'c.log', is_dir: false, size: 3, modified: 0 }], false);

    const state = useFileStore.getState();
    expect(state.entries.map((e) => e.name)).toEqual(['a.log', 'b.log', 'c.log']);
    expect(state.currentPath).toBe('/var/log');
    expect(state.isLoading).toBe(false);
  });

  it('holds the files waiting for the download ahead of them, in order', () => {
    const { addWaiting, removeWaiting } = useFileStore.getState();
    addWaiting('b.log');
    addWaiting('c.log');
    expect(useFileStore.getState().waiting).toEqual(['b.log', 'c.log']);
    removeWaiting('b.log');
    expect(useFileStore.getState().waiting).toEqual(['c.log']);
  });

  it('setDownloadProgress tracks download progress', () => {
    const { setDownloadProgress } = useFileStore.getState();
    setDownloadProgress('file.txt', 0.5);
    expect(useFileStore.getState().downloads['file.txt']).toBe(0.5);

    setDownloadProgress('file.txt', 1.0);
    expect(useFileStore.getState().downloads['file.txt']).toBe(1.0);
  });

  it('clearDownload removes only the named entry, leaving siblings intact', () => {
    const { setDownloadProgress, clearDownload } = useFileStore.getState();
    setDownloadProgress('file.txt', 0.5);
    setDownloadProgress('other.txt', 0.9);
    clearDownload('file.txt');
    // Pin both: target removed, sibling preserved — kills `filter(() => false)`
    // and `filter(() => undefined)` mutants which would either keep or drop both.
    expect(useFileStore.getState().downloads['file.txt']).toBeUndefined();
    expect(useFileStore.getState().downloads['other.txt']).toBe(0.9);
  });

  it('setUploadProgress tracks upload progress', () => {
    const { setUploadProgress } = useFileStore.getState();
    setUploadProgress('upload.bin', 0.75);
    expect(useFileStore.getState().uploads['upload.bin']).toBe(0.75);
  });

  it('clearUpload removes only the named entry, leaving siblings intact', () => {
    const { setUploadProgress, clearUpload } = useFileStore.getState();
    setUploadProgress('upload.bin', 0.5);
    setUploadProgress('other.bin', 0.25);
    clearUpload('upload.bin');
    expect(useFileStore.getState().uploads['upload.bin']).toBeUndefined();
    expect(useFileStore.getState().uploads['other.bin']).toBe(0.25);
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
