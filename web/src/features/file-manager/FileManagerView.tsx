import { useEffect, useRef } from 'react';
import { useConnectionStore } from '../../state/connection-store';
import { useFileStore } from '../../state/file-store';
import { useFileManager } from './use-file-manager';

export function FileManagerView() {
  const connectionState = useConnectionStore((s) => s.state);
  const currentPath = useFileStore((s) => s.currentPath);
  const entries = useFileStore((s) => s.entries);
  const fileError = useFileStore((s) => s.error);
  const downloads = useFileStore((s) => s.downloads);
  const { requestDirectory } = useFileManager();
  const initialRequestSent = useRef(false);

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

  return (
    <div className="p-4 space-y-4">
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

      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-gray-400 border-b border-gray-700">
            <th className="py-2 pr-4">Name</th>
            <th className="py-2 pr-4">Size</th>
            <th className="py-2">Modified</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((entry) => (
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
                {downloads[entry.name] !== undefined && (
                  <progress
                    value={Math.round(downloads[entry.name]! * 100)}
                    max={100}
                    className="mt-1 h-1 w-full rounded-full overflow-hidden [&::-webkit-progress-bar]:bg-gray-700 [&::-webkit-progress-value]:bg-blue-500 [&::-moz-progress-bar]:bg-blue-500"
                  />
                )}
              </td>
              <td className="py-2 pr-4 text-gray-400">
                {entry.is_dir ? '-' : formatSize(entry.size)}
              </td>
              <td className="py-2 text-gray-400">
                {new Date(entry.modified * 1000).toLocaleString()}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}
