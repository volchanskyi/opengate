import { useState } from 'react';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { PlayIcon } from '../../components/icons';
import { focusSessionTab, holderLine, holdsSessionTab, openSessionTab, useSessionStore } from '../session';

type SessionHolder = components['schemas']['SessionHolder'];

interface SessionControlsProps {
  deviceId: string;
  session?: SessionHolder;
  userId: string | undefined;
  isAdmin: boolean;
  /** Called once an administrator has ended the session, so the page reads the host again. */
  onEnded: () => void;
}

/**
 * What a host's session lets this technician do: open it when free, reach or move their own, and
 * see who holds anyone else's — which only an administrator may end.
 */
export function SessionControls({ deviceId, session, userId, isAdmin, onEnded }: Readonly<SessionControlsProps>) {
  const endDeviceSession = useSessionStore((s) => s.endDeviceSession);
  const [confirming, setConfirming] = useState(false);
  const [cannotFocus, setCannotFocus] = useState(false);

  if (!session) {
    return (
      <button
        type="button"
        onClick={() => openSessionTab(deviceId)}
        aria-label="Open session"
        title="Open session"
        className="px-2.5 py-1.5 bg-green-500 hover:bg-green-600 rounded text-xs font-medium inline-flex items-center"
      >
        <PlayIcon />
      </button>
    );
  }

  const badge = <span className="text-xs text-amber-300">{holderLine(session.holder_name, session.started_at)}</span>;

  if (session.holder_id === userId) {
    if (holdsSessionTab(deviceId)) {
      return (
        <span className="inline-flex items-center gap-2">
          {badge}
          <button
            type="button"
            onClick={() => fireAndForget(focusSessionTab(deviceId).then((came) => setCannotFocus(!came)))}
            className="px-2.5 py-1.5 bg-green-600 hover:bg-green-500 rounded text-xs font-medium"
          >
            Go to session
          </button>
          {cannotFocus && <span className="text-xs text-gray-400">Open in another tab of this browser</span>}
        </span>
      );
    }
    return (
      <span className="inline-flex items-center gap-2">
        {badge}
        <button
          type="button"
          onClick={() => openSessionTab(deviceId)}
          className="px-2.5 py-1.5 bg-green-600 hover:bg-green-500 rounded text-xs font-medium"
        >
          Move session here
        </button>
      </span>
    );
  }

  const end = async () => {
    setConfirming(false);
    if (await endDeviceSession(deviceId)) onEnded();
  };

  return (
    <span className="inline-flex items-center gap-2">
      {badge}
      <button
        type="button"
        disabled
        aria-label="Open session"
        title="Open session"
        className="px-2.5 py-1.5 bg-green-500 rounded text-xs font-medium inline-flex items-center disabled:opacity-50"
      >
        <PlayIcon />
      </button>
      {isAdmin && !confirming && (
        <button
          type="button"
          onClick={() => setConfirming(true)}
          className="px-2.5 py-1.5 bg-red-700 hover:bg-red-600 rounded text-xs font-medium"
        >
          End session
        </button>
      )}
      {isAdmin && confirming && (
        <button
          type="button"
          onClick={() => fireAndForget(end())}
          className="px-2.5 py-1.5 bg-red-600 ring-2 ring-red-300 rounded text-xs font-medium"
        >
          {`Confirm: end ${session.holder_name}’s session`}
        </button>
      )}
      {!isAdmin && <span className="text-xs text-gray-400">an administrator can end it</span>}
    </span>
  );
}
