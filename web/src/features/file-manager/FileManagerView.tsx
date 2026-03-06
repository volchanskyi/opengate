import { useConnectionStore } from '../../state/connection-store';
import { useFileStore } from '../../state/file-store';
import { useFileManager } from './use-file-manager';

export function FileManagerView() {
  const connectionState = useConnectionStore((s) => s.state);
  const currentPath = useFileStore((s) => s.currentPath);
  const entries = useFileStore((s) => s.entries);
  const downloads = useFileStore((s) => s.downloads);
  const { requestDirectory } = useFileManager();

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

  return (
    <div className="p-4 space-y-4">
      <div className="flex items-center gap-2 text-sm">
        <span className="text-gray-400">Path:</span>
        <span className="font-mono text-white">{currentPath}</span>
      </div>

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
                  <div
                    role="progressbar"
                    aria-valuenow={Math.round(downloads[entry.name]! * 100)}
                    className="mt-1 h-1 bg-gray-700 rounded-full overflow-hidden"
                  >
                    <div
                      className="h-full bg-blue-500"
                      style={{ width: `${downloads[entry.name]! * 100}%` }}
                    />
                  </div>
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
