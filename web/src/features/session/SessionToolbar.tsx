import type { ConnectionState, SessionPath } from '../../lib/transport/session-channel';

interface SessionToolbarProps {
  connectionState: ConnectionState;
  /** The host and its customer, as the tab's title names them. */
  title: string;
  path: SessionPath;
  onDisconnect: () => void;
}

function stateDisplay(state: ConnectionState): { label: string; color: string } {
  switch (state) {
    case 'disconnected':
      return { label: 'Disconnected', color: 'bg-gray-500' };
    case 'connecting':
      return { label: 'Connecting...', color: 'bg-yellow-500' };
    case 'connected':
      return { label: 'Connected', color: 'bg-green-500' };
    case 'error':
      return { label: 'Error', color: 'bg-red-500' };
    default:
      return { label: 'Unknown', color: 'bg-gray-500' };
  }
}

/** Direct or Through server; hovering gives the reason a session is not direct. */
function PathLabel({ path }: Readonly<{ path: SessionPath }>) {
  if (path.direct) {
    return <span className="px-2 py-0.5 rounded text-xs bg-green-900/60 text-green-300">Direct</span>;
  }
  return (
    <span
      title={path.reason}
      aria-label={path.reason ? `Through server: ${path.reason}` : 'Through server'}
      className="px-2 py-0.5 rounded text-xs bg-gray-700 text-gray-300"
    >
      Through server
    </span>
  );
}

export function SessionToolbar({ connectionState, title, path, onDisconnect }: Readonly<SessionToolbarProps>) {
  const { label, color } = stateDisplay(connectionState);
  return (
    <div className="flex items-center justify-between px-4 py-2 bg-gray-800 border-b border-gray-700">
      <div className="flex items-center gap-3">
        <span className="text-sm font-semibold text-white">{title}</span>
        <span className={`inline-block w-2 h-2 rounded-full ${color}`} />
        <span className="text-sm text-gray-300">{label}</span>
        <PathLabel path={path} />
      </div>
      <button
        type="button"
        onClick={onDisconnect}
        className="px-3 py-1 text-sm bg-gray-700 hover:bg-gray-600 rounded"
      >
        Disconnect
      </button>
    </div>
  );
}
