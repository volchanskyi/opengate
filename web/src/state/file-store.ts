import { create } from 'zustand';
import type { FileEntry } from '../lib/protocol/types';

interface FileState {
  currentPath: string;
  entries: FileEntry[];
  isLoading: boolean;
  error: string | null;
  downloads: Record<string, number>;
  uploads: Record<string, number>;

  setEntries: (path: string, entries: FileEntry[]) => void;
  setDownloadProgress: (name: string, progress: number) => void;
  clearDownload: (name: string) => void;
  setUploadProgress: (name: string, progress: number) => void;
  clearUpload: (name: string) => void;
  setError: (error: string | null) => void;
  setLoading: (loading: boolean) => void;
}

export const useFileStore = create<FileState>((set) => ({
  currentPath: '/',
  entries: [],
  isLoading: false,
  error: null,
  downloads: {},
  uploads: {},

  setEntries: (path, entries) => set({ currentPath: path, entries, isLoading: false }),

  setDownloadProgress: (name, progress) =>
    set((state) => ({ downloads: { ...state.downloads, [name]: progress } })),

  clearDownload: (name) =>
    set((state) => {
      const rest = { ...state.downloads };
      delete rest[name];
      return { downloads: rest };
    }),

  setUploadProgress: (name, progress) =>
    set((state) => ({ uploads: { ...state.uploads, [name]: progress } })),

  clearUpload: (name) =>
    set((state) => {
      const rest = { ...state.uploads };
      delete rest[name];
      return { uploads: rest };
    }),

  setError: (error) => set({ error }),
  setLoading: (isLoading) => set({ isLoading }),
}));
