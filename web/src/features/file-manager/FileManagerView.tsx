import { useEffect, useRef } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useConnectionStore } from '../session';
import { useFileStore } from './state/file-store';
import { useFileManager } from './use-file-manager';

// A listing row's height: one line of small text and its vertical padding.
const ROW_HEIGHT = 37;

export function FileManagerView() {
  const connectionState = useConnectionStore((s) => s.state);
  const currentPath = useFileStore((s) => s.currentPath);
  const entries = useFileStore((s) => s.entries);
  const fileError = useFileStore((s) => s.error);
  const downloads = useFileStore((s) => s.downloads);
  const waiting = useFileStore((s) => s.waiting);
  const viewingFile = useFileStore((s) => s.viewingFile);
  const clearViewingFile = useFileStore((s) => s.clearViewingFile);
  const { requestDirectory, requestDownload, requestView } = useFileManager();
  const initialRequestSent = useRef(false);
  const listRef = useRef<HTMLDivElement>(null);
  // Only the rows in view render, so a 120 000-entry folder pages in without stalling the tab.
  const rows = useVirtualizer({
    count: entries.length,
    getScrollElement: () => listRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 10,
  });

  // Request initial directory listing when connected
  useEffect(() => {
    if (connectionState === 'connected' && !initialRequestSent.current) {
      initialRequestSent.current = true;
      requestDirectory('/');
    }
  }, [connectionState, requestDirectory]);

  if (connectionState !== 'connected') {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-gray-400">Waiting for connection...</p>
      </div>
    );
  }

  const navigateToDir = (dirName: string) => {
    const newPath = currentPath === '/' ? `/${dirName}` : `${currentPath}/${dirName}`;
    requestDirectory(newPath);
  };

  const navigateUp = () => {
    if (currentPath === '/') return;
    const parent = currentPath.replace(/\/[^/]+$/, '') || '/';
    requestDirectory(parent);
  };

  const fullPath = (name: string) =>
    `${currentPath === '/' ? '' : currentPath}/${name}`;

  const inView = rows.getVirtualItems();
  const above = inView[0]?.start ?? 0;
  const below = rows.getTotalSize() - (inView.at(-1)?.end ?? 0);

  return (
    <div className="p-4 flex flex-col gap-4 h-full">
      <div className="flex items-center gap-2 text-sm">
        {currentPath !== '/' && (
          <button
            type="button"
            onClick={navigateUp}
            className="px-2 py-1 bg-gray-700 hover:bg-gray-600 rounded text-xs"
          >
            ..
          </button>
        )}
        <span className="text-gray-400">Path:</span>
        <span className="font-mono text-white">{currentPath}</span>
      </div>

      {fileError && (
        <div className="bg-red-900/50 border border-red-700 rounded px-3 py-2 text-sm text-red-300">
          {fileError}
        </div>
      )}

      <Transfers downloads={downloads} waiting={waiting} />

      {viewingFile && (
        <div className="border border-gray-700 rounded bg-gray-900">
          <div className="flex items-center justify-between px-3 py-2 border-b border-gray-700">
            <span className="text-sm font-mono text-white">{viewingFile.name}</span>
            <button
              type="button"
              onClick={clearViewingFile}
              aria-label="Close viewer"
              className="px-2 py-0.5 text-xs bg-gray-700 hover:bg-gray-600 rounded"
            >
              Close
            </button>
          </div>
          <pre className="p-3 text-sm text-gray-300 overflow-auto max-h-80 whitespace-pre-wrap wrap-break-word">
            {viewingFile.content}
          </pre>
        </div>
      )}

      <div ref={listRef} role="region" aria-label="Folder contents" className="flex-1 min-h-0 overflow-auto">
        <table className="w-full text-sm">
          <thead className="sticky top-0 bg-gray-900">
            <tr className="text-left text-gray-400 border-b border-gray-700">
              <th className="py-2 pr-4">Name</th>
              <th className="py-2 pr-4">Size</th>
              <th className="py-2 pr-4">Modified</th>
              <th className="py-2">Actions</th>
            </tr>
          </thead>
          <tbody>
            {above > 0 && <tr aria-hidden="true" style={{ height: above }} />}
            {inView.map(({ index }) => {
              const entry = entries.at(index);
              if (!entry) return null;
              const isWaiting = waiting.includes(entry.name);
              const isTransferring = downloads[entry.name] !== undefined || isWaiting;
              return (
                <tr key={entry.name} className="border-b border-gray-800 hover:bg-gray-800/50">
                  <td className="py-2 pr-4">
                    {entry.is_dir ? (
                      <button
                        type="button"
                        onClick={() => navigateToDir(entry.name)}
                        className="text-blue-400 hover:text-blue-300"
                      >
                        {entry.name}
                      </button>
                    ) : (
                      <span>{entry.name}</span>
                    )}
                    {isWaiting && <span className="ml-2 text-xs text-gray-500">waiting</span>}
                  </td>
                  <td className="py-2 pr-4 text-gray-400">
                    {entry.is_dir ? '-' : formatSize(entry.size)}
                  </td>
                  <td className="py-2 pr-4 text-gray-400">
                    {new Date(entry.modified * 1000).toLocaleString()}
                  </td>
                  <td className="py-2">
                    {!entry.is_dir && (
                      <span className="flex items-center gap-1">
                        <button
                          type="button"
                          onClick={() => requestView(fullPath(entry.name))}
                          disabled={isTransferring}
                          aria-label={`View ${entry.name}`}
                          className="px-2 py-0.5 text-xs bg-gray-700 hover:bg-gray-600 rounded disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          View
                        </button>
                        <button
                          type="button"
                          onClick={() => requestDownload(fullPath(entry.name))}
                          disabled={isTransferring}
                          aria-label={`Download ${entry.name}`}
                          className="px-2 py-0.5 text-xs bg-gray-700 hover:bg-gray-600 rounded disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          Download
                        </button>
                      </span>
                    )}
                  </td>
                </tr>
              );
            })}
            {below > 0 && <tr aria-hidden="true" style={{ height: below }} />}
          </tbody>
        </table>
      </div>
    </div>
  );
}

/** The running download and what waits behind it, shown whichever folder is open. */
function Transfers({ downloads, waiting }: Readonly<{ downloads: Record<string, number>; waiting: string[] }>) {
  const running = Object.entries(downloads);
  if (running.length === 0 && waiting.length === 0) return null;
  return (
    <div role="status" aria-label="Transfers" className="text-xs text-gray-300 space-y-1">
      {running.map(([name, share]) => (
        <div key={name} className="flex items-center gap-2">
          <span className="font-mono truncate">{name}</span>
          <progress
            aria-label={`Downloading ${name}`}
            value={Math.round(share * 100)}
            max={100}
            className="h-1 flex-1 rounded-full overflow-hidden [&::-webkit-progress-bar]:bg-gray-700 [&::-webkit-progress-value]:bg-blue-500 [&::-moz-progress-bar]:bg-blue-500"
          />
        </div>
      ))}
      {waiting.length > 0 && <div className="text-gray-500">Waiting: {waiting.join(', ')}</div>}
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}
