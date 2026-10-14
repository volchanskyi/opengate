import { useEffect, useRef } from 'react';
import { useConnectionStore } from '../session';
import { useFileStore } from './state/file-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { openDownload, type DownloadSink } from './download-saver';
import type { ControlMessage, FileFrame } from '../../lib/protocol/types';

/** Largest file shown in the page; a larger one is downloaded instead. */
export const VIEW_LIMIT = 1024 * 1024;

type TransferMode = 'download' | 'view';

interface Transfer {
  path: string;
  name: string;
  mode: TransferMode;
}

/** The one transfer the host is reading; its pieces pass on as they arrive, never kept whole. */
interface ActiveTransfer extends Transfer {
  received: number;
  total: number | null;
  sink: DownloadSink | null;
  viewed: Uint8Array[];
  /** Set once the page has declined the file; its remaining pieces are counted and dropped. */
  declined: boolean;
}

const decoder = new TextDecoder();

function toast(message: string): void {
  useToastStore.getState().addToast(message, 'error');
}

/**
 * Wires file listings and transfers to the file store. A listing fills in page by page; a session
 * runs one transfer at a time, later requests waiting their turn.
 */
export function useFileManager() {
  const transport = useConnectionStore((s) => s.transport);
  const listenControl = useConnectionStore((s) => s.listenControl);
  const setOnFileFrame = useConnectionStore((s) => s.setOnFileFrame);

  const listingRef = useRef<{ path: string; first: boolean } | null>(null);
  const activeRef = useRef<ActiveTransfer | null>(null);
  const queueRef = useRef<Transfer[]>([]);

  useEffect(() => {
    if (!transport) return undefined;

    const startNext = () => {
      const next = queueRef.current.shift();
      if (!next) return;
      const store = useFileStore.getState();
      store.removeWaiting(next.name);
      store.setDownloadProgress(next.name, 0);
      activeRef.current = { ...next, received: 0, total: null, sink: null, viewed: [], declined: false };
      transport.sendControl({ type: 'FileDownloadRequest', path: next.path });
    };

    const finish = (active: ActiveTransfer) => {
      activeRef.current = null;
      useFileStore.getState().clearDownload(active.name);
      startNext();
    };

    const begin = (active: ActiveTransfer, total: number) => {
      active.total = total;
      if (active.mode === 'view') {
        if (total > VIEW_LIMIT) {
          active.declined = true;
          toast(`${active.name} is too large to view; download it instead`);
        }
        return;
      }
      active.sink = openDownload(active.name, total);
      if (!active.sink) {
        active.declined = true;
        toast(`This browser can save files up to 64 MiB from a session; ${active.name} is larger`);
      }
    };

    const complete = (active: ActiveTransfer) => {
      if (!active.declined) {
        if (active.mode === 'download') {
          active.sink?.close();
        } else {
          useFileStore.getState().setViewingFile(active.name, decoder.decode(concat(active.viewed)));
        }
      }
      finish(active);
    };

    const stopListening = listenControl((msg: ControlMessage) => {
      const store = useFileStore.getState();
      switch (msg.type) {
        case 'FileListResponse': {
          const listing = listingRef.current;
          if (!listing || listing.path !== msg.path) return;
          const more = msg.more ?? false;
          if (listing.first) store.setEntries(msg.path, msg.entries, more);
          else store.appendEntries(msg.entries, more);
          listing.first = false;
          if (!more) listingRef.current = null;
          break;
        }
        case 'FileListError':
          listingRef.current = null;
          store.setLoading(false);
          store.setError(msg.error);
          break;
        case 'FileDownloadError': {
          const active = activeRef.current;
          toast(`Couldn't download ${msg.path.split('/').pop() ?? msg.path}: ${msg.error}`);
          if (active?.path === msg.path) {
            active.sink?.abort();
            finish(active);
          }
          break;
        }
        default:
          break;
      }
    });

    setOnFileFrame((frame: FileFrame) => {
      const active = activeRef.current;
      if (!active) return;
      if (active.total === null) begin(active, frame.total_size);
      active.received += frame.data.length;
      if (!active.declined) {
        if (active.mode === 'download') active.sink?.write(frame.data);
        else active.viewed.push(frame.data.slice());
      }
      const total = active.total ?? 0;
      useFileStore.getState().setDownloadProgress(active.name, total === 0 ? 1 : active.received / total);
      if (active.received >= total) complete(active);
    });

    return () => {
      activeRef.current?.sink?.abort();
      activeRef.current = null;
      queueRef.current = [];
      stopListening();
      setOnFileFrame(null);
    };
  }, [transport, listenControl, setOnFileFrame]);

  const requestDirectory = (path: string) => {
    if (!transport) return;
    listingRef.current = { path, first: true };
    const store = useFileStore.getState();
    store.setError(null);
    store.setLoading(true);
    transport.sendControl({ type: 'FileListRequest', path });
  };

  const requestTransfer = (path: string, mode: TransferMode) => {
    if (!transport) return;
    const name = path.split('/').pop() ?? 'file';
    if (activeRef.current) {
      queueRef.current.push({ path, name, mode });
      useFileStore.getState().addWaiting(name);
      return;
    }
    useFileStore.getState().setDownloadProgress(name, 0);
    activeRef.current = { path, name, mode, received: 0, total: null, sink: null, viewed: [], declined: false };
    transport.sendControl({ type: 'FileDownloadRequest', path });
  };

  const requestDownload = (path: string) => requestTransfer(path, 'download');
  const requestView = (path: string) => requestTransfer(path, 'view');

  return { requestDirectory, requestDownload, requestView };
}

function concat(parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const part of parts) {
    out.set(part, at);
    at += part.length;
  }
  return out;
}
