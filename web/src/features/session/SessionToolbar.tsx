import type { ConnectionState } from '../../lib/transport/ws-transport';

interface SessionToolbarProps {
  connectionState: ConnectionState;
  onDisconnect: () => void;
}

const STATE_CONFIG: Record<ConnectionState, { label: string; color: string }> = {
  disconnected: { label: 'Disconnected', color: 'bg-gray-500' },
  connecting:   { label: 'Connecting...', color: 'bg-yellow-500' },
  connected:    { label: 'Connected', color: 'bg-green-500' },
  error:        { label: 'Error', color: 'bg-red-500' },
};

export function SessionToolbar({ connectionState, onDisconnect }: SessionToolbarProps) {
  return (
    <div className="flex items-center justify-between px-4 py-2 bg-gray-800 border-b border-gray-700">
      <div className="flex items-center gap-2">
        <span className={`inline-block w-2 h-2 rounded-full ${STATE_CONFIG[connectionState].color}`} />
        <span className="text-sm text-gray-300">{STATE_CONFIG[connectionState].label}</span>
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
