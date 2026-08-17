import type { components } from '../../types/api';
import { shortId } from '../../lib/short-id';
import { eventLine, formatMoment } from './incident-format';

type IncidentEvent = components['schemas']['IncidentEvent'];

interface Props {
  readonly events: readonly IncidentEvent[];
  /** How many lines the room holds altogether. */
  readonly total: number;
}

/**
 * How a room got to where it stands — what a handover between two technicians
 * reads. Every line's free text is rendered as text: a comment carries whatever
 * somebody typed, and the only safe treatment of that is characters on a screen.
 */
export function IncidentTimeline({ events, total }: Props) {
  if (events.length === 0) {
    return <p className="text-sm text-gray-500">Nothing has happened in this room yet.</p>;
  }

  return (
    <div className="space-y-2">
      {total > events.length && (
        <p className="text-xs text-gray-500">Showing {events.length} of {total} lines.</p>
      )}
      <ol aria-label="Timeline" className="space-y-2">
        {events.map((event) => {
          const line = eventLine(event);
          return (
            <li key={event.id} className="border-l-2 border-gray-700 pl-3">
              <p className="text-sm text-gray-200">{line.title}</p>
              {line.quote !== '' && (
                <p className="text-sm text-gray-300 bg-gray-900 border border-gray-700 rounded px-2 py-1 mt-1 whitespace-pre-wrap wrap-break-word">
                  {line.quote}
                </p>
              )}
              <p className="text-xs text-gray-500">
                {formatMoment(event.at)} · {event.actor_id ? `by ${shortId(event.actor_id)}` : 'by the system'}
              </p>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
