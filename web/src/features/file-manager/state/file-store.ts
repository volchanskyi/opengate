import { create } from 'zustand';
import type { FileEntry } from '../../../lib/protocol/types';

interface FileState {
  currentPath: string;
  entries: FileEntry[];
  isLoading: boolean;
  error: string | null;
  downloads: Record<string, number>;
  /** Files whose download or view waits for the one running; a session runs one at a time. */
  waiting: string[];
  uploads: Record<string, number>;
  viewingFile: { name: string; content: string } | null;

  /** Starts a listing with its first page; it stays loading while more pages are to come. */
  setEntries: (path: string, entries: FileEntry[], more?: boolean) => void;
  appendEntries: (entries: FileEntry[], more: boolean) => void;
  addWaiting: (name: string) => void;
  removeWaiting: (name: string) => void;
  setDownloadProgress: (name: string, progress: number) => void;
  clearDownload: (name: string) => void;
  setUploadProgress: (name: string, progress: number) => void;
  clearUpload: (name: string) => void;
  setError: (error: string | null) => void;
  setLoading: (loading: boolean) => void;
  setViewingFile: (name: string, content: string) => void;
  clearViewingFile: () => void;
}

export const useFileStore = create<FileState>((set) => ({
  currentPath: '/',
  entries: [],
  isLoading: false,
  error: null,
  downloads: {},
  waiting: [],
  uploads: {},
  viewingFile: null,

  setEntries: (path, entries, more = false) => set({ currentPath: path, entries, isLoading: more }),
  appendEntries: (entries, more) => set((state) => ({ entries: [...state.entries, ...entries], isLoading: more })),
  addWaiting: (name) => set((state) => ({ waiting: [...state.waiting, name] })),
  removeWaiting: (name) => set((state) => ({ waiting: state.waiting.filter((n) => n !== name) })),

  setDownloadProgress: (name, progress) =>
    set((state) => ({ downloads: { ...state.downloads, [name]: progress } })),

  clearDownload: (name) =>
    set((state) => ({
      downloads: Object.fromEntries(Object.entries(state.downloads).filter(([k]) => k !== name)),
    })),

  setUploadProgress: (name, progress) =>
    set((state) => ({ uploads: { ...state.uploads, [name]: progress } })),

  clearUpload: (name) =>
    set((state) => ({
      uploads: Object.fromEntries(Object.entries(state.uploads).filter(([k]) => k !== name)),
    })),

  setError: (error) => set({ error }),
  setLoading: (isLoading) => set({ isLoading }),
  setViewingFile: (name, content) => set({ viewingFile: { name, content } }),
  clearViewingFile: () => set({ viewingFile: null }),
}));
