import { useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router';
import { api } from '../../lib/api';
import { useConnectionStore } from './state/connection-store';
import { useSessionStore } from './state/session-store';
import { useAuthStore } from '../../state/auth-store';
import { SessionToolbar } from './SessionToolbar';
import { ToastContainer } from '../../components/ToastContainer';
import { RemoteDesktopView } from '../remote-desktop';
import { TerminalView } from '../terminal';
import { FileManagerView } from '../file-manager';
import { MessengerView } from '../messenger';
import { fireAndForget } from '../../lib/fire-and-forget';
import { announceSessionTab, sessionTabName, watchSignIn } from './session-tab';
import { identityOf, noteRequest } from '../../lib/console-session';

/** How often an open live session counts as activity, matching the server's renewal. */
const LIVE_SESSION_ACTIVITY_MS = 60_000;
import { holderLine } from './holder';

const ALL_TABS = ['Desktop', 'Terminal', 'Files', 'Chat'] as const;
type Tab = (typeof ALL_TABS)[number];

/** Where the tab is: opening, open, refused while another technician holds the host, or over. */
type Phase =
  | { kind: 'starting' }
  | { kind: 'open' }
  | { kind: 'held'; holderName: string; startedAt: string }
  | { kind: 'failed'; error: string }
  | { kind: 'ended'; reason: string };

/** Determine which tabs to show based on device capabilities. */
function availableTabs(capabilities?: string[]): readonly Tab[] {
  const hasDesktop = capabilities?.includes('RemoteDesktop') ?? false;
  return ALL_TABS.filter((tab) =>
    tab === 'Desktop' || tab === 'Chat' ? hasDesktop : true,
  );
}

interface HostRecord {
  hostname: string;
  customer: string;
  capabilities: string[];
}

/** The host's name, its customer's name and its capabilities, read from the device record alone. */
async function readHost(deviceId: string): Promise<HostRecord | null> {
  const device = await api.GET('/api/v1/devices/{id}', { params: { path: { id: deviceId } } });
  if (!device.data) return null;
  const organization = await api.GET('/api/v1/organizations/{id}', {
    params: { path: { id: device.data.organization_id } },
  });
  return {
    hostname: device.data.hostname,
    customer: organization.data?.name ?? '',
    capabilities: device.data.capabilities,
  };
}

/**
 * One host's session in a tab of its own: it opens the session, needs nothing from the page that
 * opened it, and stands apart from the fleet views and the customer chosen there.
 */
export function SessionView() {
  const { id = '' } = useParams<{ id: string }>();
  const connectionState = useConnectionStore((s) => s.state);
  const path = useConnectionStore((s) => s.path);
  const closedReason = useConnectionStore((s) => s.closedReason);
  const connect = useConnectionStore((s) => s.connect);
  const disconnect = useConnectionStore((s) => s.disconnect);
  const startSession = useSessionStore((s) => s.startSession);

  const [host, setHost] = useState<HostRecord | null>(null);
  const [phase, setPhase] = useState<Phase>({ kind: 'starting' });
  const title = host ? `${host.hostname} — ${host.customer}` : 'Session';

  useEffect(() => {
    window.name = sessionTabName(id);
  }, [id]);

  useEffect(() => {
    document.title = title;
  }, [title]);

  useEffect(() => {
    let current = true;
    fireAndForget(readHost(id).then((record) => current && setHost(record)));
    return () => {
      current = false;
    };
  }, [id]);

  // The console's session cookie opens the relay, so only a signed-in console connects.
  useEffect(() => {
    let current = true;
    fireAndForget(
      startSession(id).then((result) => {
        if (!current || !useAuthStore.getState().user) return;
        if (result.kind === 'opened') {
          connect(result.token, result.relayUrl, result.helpers);
          setPhase({ kind: 'open' });
        } else {
          setPhase(result);
        }
      }),
    );
    return () => {
      current = false;
      disconnect();
    };
  }, [id, startSession, connect, disconnect]);

  // The relay's close reason ends an open session: "Opened elsewhere", "Ended by <name>".
  const shown: Phase = phase.kind === 'open' && closedReason !== null ? { kind: 'ended', reason: closedReason } : phase;
  const open = shown.kind === 'open';

  useEffect(() => (open ? announceSessionTab(id) : undefined), [open, id]);

  // The watch holds the account the session opened under; another account or none ends it.
  useEffect(() => {
    const { user, tenantId } = useAuthStore.getState();
    if (!open || !user || !tenantId) return undefined;
    return watchSignIn(identityOf(user.id, tenantId), () => {
      disconnect();
      setPhase({ kind: 'ended', reason: 'Signed out' });
    });
  }, [open, disconnect]);

  // An open session keeps the server's idle clock running, so the console's own clock follows it.
  useEffect(() => {
    if (!open) return undefined;
    const timer = setInterval(() => noteRequest(), LIVE_SESSION_ACTIVITY_MS);
    return () => clearInterval(timer);
  }, [open]);

  const handleDisconnect = () => {
    disconnect();
    setPhase({ kind: 'ended', reason: 'Session ended' });
    // Closing works for a tab this application opened; any other stays showing the reason.
    window.close();
  };

  return (
    <div className="flex flex-col h-screen bg-gray-900 text-white">
      <SessionToolbar connectionState={connectionState} title={title} path={path} onDisconnect={handleDisconnect} />
      <SessionBody phase={shown} capabilities={host?.capabilities} />
      {/* The tab stands outside the fleet layout, so it shows its own features' messages. */}
      <ToastContainer />
    </div>
  );
}

function SessionBody({ phase, capabilities }: Readonly<{ phase: Phase; capabilities?: string[] }>) {
  switch (phase.kind) {
    case 'starting':
      return <Notice text="Opening session…" />;
    case 'held':
      return <Notice text={holderLine(phase.holderName, phase.startedAt)} />;
    case 'failed':
      return <Notice text={phase.error} />;
    case 'ended':
      return <Notice text={phase.reason} />;
    case 'open':
      return <SessionTabs capabilities={capabilities} />;
  }
}

function Notice({ text }: Readonly<{ text: string }>) {
  return <div className="p-6 text-sm text-gray-300">{text}</div>;
}

function SessionTabs({ capabilities }: Readonly<{ capabilities?: string[] }>) {
  const tabs = useMemo(() => availableTabs(capabilities), [capabilities]);
  const [activeTab, setActiveTab] = useState<Tab>('Terminal');

  return (
    <>
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

      {/* Every feature stays mounted, so a download carries on while the terminal is shown. */}
      {tabs.map((tab) => (
        <div key={tab} role="tabpanel" hidden={activeTab !== tab} className="flex-1 overflow-hidden">
          <TabContent tab={tab} />
        </div>
      ))}
    </>
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
