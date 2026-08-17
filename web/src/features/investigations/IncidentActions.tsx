import { useState } from 'react';
import type { components } from '../../types/api';
import { shortId } from '../../lib/short-id';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useAuthStore } from '../../state/auth-store';
import {
  CAUSE_CODES,
  allowedNextStatuses,
  causeLabel,
  requiresCause,
  statusLabel,
} from './incident-lifecycle';
import { useRoomStore } from './state/room-store';

type Incident = components['schemas']['Incident'];
type Status = components['schemas']['IncidentStatus'];
type CauseCode = components['schemas']['IncidentCauseCode'];

const BUTTON = 'px-3 py-1.5 rounded text-xs font-medium disabled:opacity-50';

function Assignment({ incident, disabled }: { readonly incident: Incident; readonly disabled: boolean }) {
  const me = useAuthStore((s) => s.user?.id);
  const setAssignee = useRoomStore((s) => s.setAssignee);
  const holder = incident.assignee_id ?? null;
  const mine = holder !== null && holder === me;

  let label = 'Take it';
  if (mine) label = 'Hand it back';
  else if (holder !== null) label = 'Take it over';

  return (
    <div className="flex items-center gap-2">
      <span className="text-xs text-gray-400">
        {holder === null ? 'Nobody has taken this' : `Held by ${shortId(holder)}`}
      </span>
      {me !== undefined && (
        <button
          type="button"
          disabled={disabled}
          onClick={() => { fireAndForget(setAssignee(incident.id, mine ? null : me)); }}
          className={`${BUTTON} bg-gray-700 hover:bg-gray-600`}
        >
          {label}
        </button>
      )}
    </div>
  );
}

function NoteBox({ incidentId, disabled }: { readonly incidentId: string; readonly disabled: boolean }) {
  const [note, setNote] = useState('');
  const addComment = useRoomStore((s) => s.addComment);

  const submit = async () => {
    // A refused note stays in the box: what somebody typed is not thrown away
    // because the server said no.
    if (await addComment(incidentId, note)) setNote('');
  };

  return (
    <form
      className="flex items-end gap-2"
      onSubmit={(e) => { e.preventDefault(); fireAndForget(submit()); }}
    >
      <label className="flex flex-col gap-1 text-xs text-gray-400 flex-1">
        <span>Add a note</span>
        <input
          value={note}
          onChange={(e) => setNote(e.target.value)}
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100"
        />
      </label>
      <button type="submit" disabled={disabled || note.trim() === ''} className={`${BUTTON} bg-blue-600 hover:bg-blue-500`}>
        Add note
      </button>
    </form>
  );
}

function ResolutionForm({ incidentId, disabled, onDone }: {
  readonly incidentId: string;
  readonly disabled: boolean;
  readonly onDone: () => void;
}) {
  const [cause, setCause] = useState('');
  const setStatus = useRoomStore((s) => s.setStatus);

  const confirm = async () => {
    if (cause === '') return;
    if (await setStatus(incidentId, 'resolved', cause as CauseCode)) onDone();
  };

  return (
    <div className="flex items-end gap-2 flex-wrap">
      <label className="flex flex-col gap-1 text-xs text-gray-400">
        <span>Why it ended</span>
        <select
          value={cause}
          onChange={(e) => setCause(e.target.value)}
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100"
        >
          <option value="">Choose an answer…</option>
          {CAUSE_CODES.map((code) => (
            <option key={code} value={code}>{causeLabel(code)}</option>
          ))}
        </select>
      </label>
      <button
        type="button"
        disabled={disabled || cause === ''}
        onClick={() => { fireAndForget(confirm()); }}
        className={`${BUTTON} bg-green-600 hover:bg-green-500`}
      >
        Confirm resolution
      </button>
      <button type="button" onClick={onDone} className={`${BUTTON} bg-gray-700 hover:bg-gray-600`}>
        Cancel
      </button>
    </div>
  );
}

/**
 * Everything a person does to a room: move it, take it, annotate it.
 *
 * Only the moves the lifecycle permits are rendered, so an illegal transition is
 * never offered rather than offered and refused. A resolution asks for its cause
 * before it is sent, because that answer is what the curated rule pack is
 * retuned from.
 *
 * There is deliberately nothing here that acts on the machine — no restart, no
 * script, no session. The room reads a frozen snapshot; acting on a machine is
 * the machine's own page.
 */
export function IncidentActions({ incident }: { readonly incident: Incident }) {
  const [resolving, setResolving] = useState(false);
  const acting = useRoomStore((s) => s.acting);
  const actionError = useRoomStore((s) => s.actionError);
  const setStatus = useRoomStore((s) => s.setStatus);

  const moves = allowedNextStatuses(incident.status);
  const plainMoves = moves.filter((s) => !requiresCause(s));
  const canResolve = moves.some(requiresCause);

  const move = (to: Status) => { fireAndForget(setStatus(incident.id, to)); };

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 flex-wrap">
        {plainMoves.map((to) => (
          <button
            key={to}
            type="button"
            disabled={acting}
            onClick={() => { move(to); }}
            className={`${BUTTON} bg-gray-700 hover:bg-gray-600`}
          >
            {statusLabel(to)}
          </button>
        ))}
        {canResolve && !resolving && (
          <button
            type="button"
            disabled={acting}
            onClick={() => setResolving(true)}
            className={`${BUTTON} bg-green-600 hover:bg-green-500`}
          >
            Resolve
          </button>
        )}
        {moves.length === 0 && incident.cause_code && (
          <p className="text-xs text-gray-400">Resolved — {causeLabel(incident.cause_code)}</p>
        )}
      </div>

      {resolving && (
        <ResolutionForm incidentId={incident.id} disabled={acting} onDone={() => setResolving(false)} />
      )}

      <Assignment incident={incident} disabled={acting} />
      <NoteBox incidentId={incident.id} disabled={acting} />

      {actionError && <p role="alert" className="text-xs text-red-400">{actionError}</p>}
    </div>
  );
}
