import { useEffect, useRef } from 'react';
import { useConnectionStore } from '../../state/connection-store';
import { useFileStore } from '../../state/file-store';
import { DownloadAccumulator } from './file-transfer';
import type { FileFrame } from '../../lib/protocol/types';

type TransferMode = 'download' | 'view';

interface ActiveTransfer {
  name: string;
  mode: TransferMode;
  accumulator: DownloadAccumulator | null;
}

/** Trigger browser "Save As" for a Blob. */
function triggerBrowserSave(filename: string, blob: Blob): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

/** Hook that wires transport control messages and file frames to the file store. */
export function useFileManager() {
  const transport = useConnectionStore((s) => s.transport);
  const setOnControlMessage = useConnectionStore((s) => s.setOnControlMessage);
  const setOnFileFrame = useConnectionStore((s) => s.setOnFileFrame);
  const setEntries = useFileStore((s) => s.setEntries);
  const setLoading = useFileStore((s) => s.setLoading);

  const activeTransferRef = useRef<ActiveTransfer | null>(null);

  useEffect(() => {
    if (!transport) return;

    setOnControlMessage((msg) => {
      if (msg.type === 'FileListResponse') {
        setEntries(msg.path, msg.entries);
      } else if (msg.type === 'FileListError') {
        useFileStore.getState().setLoading(false);
        useFileStore.getState().setError(msg.error);
      }
    });

    setOnFileFrame((frame: FileFrame) => {
      const transfer = activeTransferRef.current;
      if (!transfer) return;

      // Lazily create accumulator on first frame
      if (!transfer.accumulator) {
        transfer.accumulator = new DownloadAccumulator(frame.total_size);
      }

      transfer.accumulator.addChunk(frame);
      useFileStore.getState().setDownloadProgress(transfer.name, transfer.accumulator.progress());

      if (transfer.accumulator.isComplete()) {
        const blob = transfer.accumulator.toBlob();
        const { name, mode } = transfer;
        activeTransferRef.current = null;

        if (mode === 'download') {
          triggerBrowserSave(name, blob);
          useFileStore.getState().clearDownload(name);
        } else {
          blob.text().then((text) => {
            useFileStore.getState().setViewingFile(name, text);
            useFileStore.getState().clearDownload(name);
          });
        }
      }
    });

    return () => {
      setOnControlMessage(null);
      setOnFileFrame(null);
    };
  }, [transport, setOnControlMessage, setOnFileFrame, setEntries, setLoading]);

  const requestDirectory = (path: string) => {
    if (!transport) return;
    useFileStore.getState().setError(null);
    useFileStore.getState().setLoading(true);
    transport.sendControl({ type: 'FileListRequest', path });
  };

  const requestDownload = (path: string) => {
    if (!transport) return;
    const name = path.split('/').pop() ?? 'download';
    activeTransferRef.current = { name, mode: 'download', accumulator: null };
    useFileStore.getState().setDownloadProgress(name, 0);
    transport.sendControl({ type: 'FileDownloadRequest', path });
  };

  const requestView = (path: string) => {
    if (!transport) return;
    const name = path.split('/').pop() ?? 'file';
    activeTransferRef.current = { name, mode: 'view', accumulator: null };
    useFileStore.getState().setDownloadProgress(name, 0);
    transport.sendControl({ type: 'FileDownloadRequest', path });
  };

  return { requestDirectory, requestDownload, requestView };
}
