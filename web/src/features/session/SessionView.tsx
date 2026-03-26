import { useEffect, useMemo, useState } from 'react';
import { useParams, useLocation, useNavigate } from 'react-router-dom';
import { useConnectionStore } from '../../state/connection-store';
import { useAuthStore } from '../../state/auth-store';
import { SessionToolbar } from './SessionToolbar';
import { RemoteDesktopView } from '../remote-desktop/RemoteDesktopView';
import { TerminalView } from '../terminal/TerminalView';
import { FileManagerView } from '../file-manager/FileManagerView';
import { MessengerView } from '../messenger/MessengerView';

const ALL_TABS = ['Desktop', 'Terminal', 'Files', 'Chat'] as const;
type Tab = (typeof ALL_TABS)[number];

/** Determine which tabs to show based on device capabilities. */
function availableTabs(capabilities?: string[]): readonly Tab[] {
  if (!capabilities || capabilities.length === 0) {
    // Fallback: show all tabs when capabilities are unknown (legacy agents)
    return ALL_TABS;
  }
  const hasDesktop = capabilities.includes('RemoteDesktop');
  return ALL_TABS.filter((tab) =>
    tab === 'Desktop' || tab === 'Chat' ? hasDesktop : true,
  );
}

export function SessionView() {
  const { token } = useParams<{ token: string }>();
  const location = useLocation();
  const navigate = useNavigate();
  const locState = location.state as { relayUrl?: string; capabilities?: string[] } | null;
  const relayUrl = locState?.relayUrl ?? '';
  const capabilities = locState?.capabilities;

  const connectionState = useConnectionStore((s) => s.state);
  const connectionError = useConnectionStore((s) => s.error);
  const connect = useConnectionStore((s) => s.connect);
  const disconnect = useConnectionStore((s) => s.disconnect);
  const authToken = useAuthStore((s) => s.token);

  const tabs = useMemo(() => availableTabs(capabilities), [capabilities]);
  const [activeTab, setActiveTab] = useState<Tab>(tabs.includes('Terminal') ? 'Terminal' : tabs[0]!);

  useEffect(() => {
    if (token && relayUrl && authToken) {
      connect(token, relayUrl, authToken);
    }
    return () => {
      disconnect();
    };
    // Only run on mount/unmount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleDisconnect = () => {
    disconnect();
    navigate('/devices');
  };

  return (
    <div className="flex flex-col h-[calc(100vh-52px)]">
      <SessionToolbar connectionState={connectionState} onDisconnect={handleDisconnect} />

      {connectionError && (
        <div className="px-4 py-2 bg-red-900/50 border-b border-red-700 text-sm text-red-300">
          {connectionError}
        </div>
      )}

      <div className="flex border-b border-gray-700" role="tablist">
        {tabs.map((tab) => (
          <button
            key={tab}
            role="tab"
            type="button"
            aria-selected={activeTab === tab}
            onClick={() => setActiveTab(tab)}
            className={`px-4 py-2 text-sm font-medium ${
              activeTab === tab
                ? 'text-white border-b-2 border-blue-500'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            {tab}
          </button>
        ))}
      </div>

      <div role="tabpanel" className="flex-1 overflow-hidden">
        <TabContent tab={activeTab} />
      </div>
    </div>
  );
}

function TabContent({ tab }: Readonly<{ tab: Tab }>) {
  switch (tab) {
    case 'Desktop':
      return <RemoteDesktopView />;
    case 'Terminal':
      return <TerminalView />;
    case 'Files':
      return <FileManagerView />;
    case 'Chat':
      return <MessengerView />;
  }
}
