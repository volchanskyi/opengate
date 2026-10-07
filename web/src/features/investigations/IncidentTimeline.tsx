import type { components } from '../../types/api';
import { eventLine, formatMoment, personName, type People } from './incident-format';

type IncidentEvent = components['schemas']['IncidentEvent'];

interface Props {
  readonly events: readonly IncidentEvent[];
  /** How many lines the room holds altogether. */
  readonly total: number;
  /** Display names of the people the lines name. */
  readonly people: People;
}

/** A room's history; every line's free text, typed by a person, is rendered as plain text. */
export function IncidentTimeline({ events, total, people }: Props) {
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
          const line = eventLine(event, people);
          return (
            <li key={event.id} className="border-l-2 border-gray-700 pl-3">
              <p className="text-sm text-gray-200">{line.title}</p>
              {line.quote !== '' && (
                <p className="text-sm text-gray-300 bg-gray-900 border border-gray-700 rounded px-2 py-1 mt-1 whitespace-pre-wrap wrap-break-word">
                  {line.quote}
                </p>
              )}
              <p className="text-xs text-gray-500">
                {formatMoment(event.at)} · {event.actor_id ? `by ${personName(people, event.actor_id)}` : 'by the system'}
              </p>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
