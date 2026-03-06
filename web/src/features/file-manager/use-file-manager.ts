import { useEffect } from 'react';
import { useConnectionStore } from '../../state/connection-store';
import { useFileStore } from '../../state/file-store';

/** Hook that wires transport control messages to the file store. */
export function useFileManager() {
  const transport = useConnectionStore((s) => s.transport);
  const setOnControlMessage = useConnectionStore((s) => s.setOnControlMessage);
  const setEntries = useFileStore((s) => s.setEntries);
  const setLoading = useFileStore((s) => s.setLoading);

  useEffect(() => {
    if (!transport) return;

    setOnControlMessage((msg) => {
      if (msg.type === 'FileListResponse') {
        setEntries(msg.path, msg.entries);
      }
    });

    return () => {
      setOnControlMessage(null);
    };
  }, [transport, setOnControlMessage, setEntries, setLoading]);

  const requestDirectory = (path: string) => {
    if (!transport) return;
    useFileStore.getState().setLoading(true);
    transport.sendControl({ type: 'FileListRequest', path });
  };

  const requestDownload = (path: string) => {
    if (!transport) return;
    transport.sendControl({ type: 'FileDownloadRequest', path });
  };

  return { requestDirectory, requestDownload };
}
