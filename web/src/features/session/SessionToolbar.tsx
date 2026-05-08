import type { ConnectionState } from '../../lib/transport/ws-transport';

interface SessionToolbarProps {
  connectionState: ConnectionState;
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

export function SessionToolbar({ connectionState, onDisconnect }: Readonly<SessionToolbarProps>) {
  const { label, color } = stateDisplay(connectionState);
  return (
    <div className="flex items-center justify-between px-4 py-2 bg-gray-800 border-b border-gray-700">
      <div className="flex items-center gap-2">
        <span className={`inline-block w-2 h-2 rounded-full ${color}`} />
        <span className="text-sm text-gray-300">{label}</span>
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
