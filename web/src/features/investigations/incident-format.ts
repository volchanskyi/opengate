import type { components } from '../../types/api';
import { shortId } from '../../lib/short-id';
import { causeLabel, statusLabel } from './incident-lifecycle';

type IncidentEvent = components['schemas']['IncidentEvent'];
type Status = components['schemas']['IncidentStatus'];
type CauseCode = components['schemas']['IncidentCauseCode'];

const DASH = '—';
const MINUTE_MS = 60_000;
const HOUR_MS = 3_600_000;
const DAY_MS = 86_400_000;

/** One timeline entry, split so free text is rendered as text and never as a heading. */
export interface TimelineLine {
  /** What happened, in the vocabulary the lifecycle defines. */
  title: string;
  /** A person's own words, when the line carries any. */
  quote: string;
}

function instant(iso: string | null | undefined): number | null {
  if (!iso) return null;
  const ms = Date.parse(iso);
  return Number.isNaN(ms) ? null : ms;
}

/** An instant in the reader's own locale, or a dash when it cannot be read. */
export function formatMoment(iso: string | null | undefined): string {
  const ms = instant(iso);
  return ms === null ? DASH : new Date(ms).toLocaleString();
}

/**
 * How long a span ran, at the coarsest resolution that still says something. A
 * room open for two days is "2 d 4 h"; the minutes stopped mattering long ago.
 */
export function durationLabel(fromISO: string | null | undefined, toISO: string | null | undefined): string {
  const from = instant(fromISO);
  const to = instant(toISO);
  if (from === null || to === null) return DASH;

  const ms = Math.max(0, to - from);
  if (ms < MINUTE_MS) return 'under a minute';
  if (ms < HOUR_MS) return `${String(Math.floor(ms / MINUTE_MS))} m`;
  if (ms < DAY_MS) {
    const hours = Math.floor(ms / HOUR_MS);
    const minutes = Math.floor((ms % HOUR_MS) / MINUTE_MS);
    return minutes === 0 ? `${String(hours)} h` : `${String(hours)} h ${String(minutes)} m`;
  }
  const days = Math.floor(ms / DAY_MS);
  const hours = Math.floor((ms % DAY_MS) / HOUR_MS);
  return hours === 0 ? `${String(days)} d` : `${String(days)} d ${String(hours)} h`;
}

/** A count with the noun that agrees with it. */
export function countLabel(n: number, singular: string, plural: string): string {
  return `${String(n)} ${n === 1 ? singular : plural}`;
}

/**
 * What an alert read, when it read anything. A rule that fires on an event
 * rather than a threshold carries no metric, and saying so beats printing a
 * blank where a number belongs.
 */
export function alertReadingLabel(metric: string | undefined, value: number | null | undefined): string {
  if (!metric) return DASH;
  if (value === null || value === undefined) return metric;
  return `${metric} ${String(Number(value.toFixed(2)))}`;
}

/** Read one field of an untyped event body without indexing it by a variable. */
function bodyReader(body: Record<string, unknown>) {
  const fields = new Map(Object.entries(body));
  return {
    text: (key: string): string => {
      const value = fields.get(key);
      return typeof value === 'string' ? value : '';
    },
    flag: (key: string): boolean => fields.get(key) === true,
  };
}

function statusMove(from: string, to: string): string {
  // The body carries wire values; an unknown one falls through the label maps
  // and renders as itself rather than disappearing.
  return `${statusLabel(from as Status)} → ${statusLabel(to as Status)}`;
}

function transitionTitle(read: ReturnType<typeof bodyReader>): string {
  const from = read.text('from');
  const to = read.text('to');
  if (!from || !to) return 'Status changed';
  const move = statusMove(from, to);
  return read.flag('reopened') ? `Reopened — ${move}` : move;
}

function resolutionTitle(read: ReturnType<typeof bodyReader>): string {
  const cause = read.text('cause_code');
  if (cause) return `Resolved — ${causeLabel(cause as CauseCode)}`;
  const reason = read.text('reason');
  if (reason) return `Resolved automatically — ${reason}`;
  return 'Resolved';
}

function assignmentTitle(read: ReturnType<typeof bodyReader>): string {
  if (read.flag('unassigned')) return 'Unassigned';
  return `Assigned to ${shortId(read.text('assignee_id'))}`;
}

/**
 * Render one line of a room's history. The incident's own fields say where it
 * stands; these say how it got there, which is what a handover between two
 * technicians reads.
 */
export function eventLine(event: IncidentEvent): TimelineLine {
  const read = bodyReader(event.body);
  switch (event.kind) {
    case 'comment':
      return { title: 'Comment', quote: read.text('body') };
    case 'assignment':
      return { title: assignmentTitle(read), quote: '' };
    case 'resolution':
      return { title: resolutionTitle(read), quote: '' };
    case 'status_change':
      return { title: transitionTitle(read), quote: '' };
    case 'alert_folded':
      return { title: 'Alert folded in', quote: '' };
    case 'device_offline':
      return { title: 'Device went offline', quote: '' };
  }
}
