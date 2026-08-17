import type { components } from '../../types/api';

type Status = components['schemas']['IncidentStatus'];
type Severity = components['schemas']['IncidentSeverity'];
type CauseCode = components['schemas']['IncidentCauseCode'];

/**
 * The lifecycle the server enforces, mirrored so the UI never offers a move that
 * will come back refused.
 *
 * Forward is the ordinary path and every skip along it is allowed — plenty of
 * incidents are picked up and closed in one move. Backward stops at the queue, so
 * a technician going off shift hands a room back rather than leaving it looking
 * worked. A resolved room has no successor: undoing an answer already given is a
 * reopen, which this surface does not offer.
 */
const NEXT_BY_STATUS = {
  new: ['acknowledged', 'investigating', 'resolved'],
  acknowledged: ['new', 'investigating', 'resolved'],
  investigating: ['new', 'acknowledged', 'resolved'],
  resolved: [],
} satisfies Record<Status, readonly Status[]>;

const STATUS_LABELS = {
  new: 'New',
  acknowledged: 'Acknowledged',
  investigating: 'Investigating',
  resolved: 'Resolved',
} satisfies Record<Status, string>;

const STATUS_TONES = {
  new: 'bg-blue-900 text-blue-200',
  acknowledged: 'bg-amber-900 text-amber-200',
  investigating: 'bg-purple-900 text-purple-200',
  resolved: 'bg-gray-700 text-gray-300',
} satisfies Record<Status, string>;

const SEVERITY_LABELS = {
  critical: 'Critical',
  warning: 'Warning',
  info: 'Info',
} satisfies Record<Severity, string>;

const SEVERITY_TONES = {
  critical: 'bg-red-900 text-red-200',
  warning: 'bg-yellow-900 text-yellow-200',
  info: 'bg-sky-900 text-sky-200',
} satisfies Record<Severity, string>;

/**
 * Why a room ended, in an operator's words. `false_positive` says so explicitly:
 * it is the one answer that feeds back into which curated rule needs its
 * threshold moved, and a label that hides that spends the feedback.
 */
const CAUSE_LABELS = {
  resolved_self: 'Resolved itself',
  fixed_by_tech: 'Fixed by a technician',
  hardware_fault: 'Hardware fault',
  expected_load: 'Expected load',
  false_positive: 'False alarm — the rule needs retuning',
  duplicate: 'Duplicate of another incident',
  wont_fix: 'Will not fix',
} satisfies Record<CauseCode, string>;

// Built from the literals above so a vocabulary the spec grows fails to compile
// here rather than rendering as a raw wire value. Map lookups throughout: a
// dynamic key against an object literal is what `security/detect-object-injection`
// exists to stop.
const NEXT = new Map<Status, readonly Status[]>(
  Object.entries(NEXT_BY_STATUS) as [Status, readonly Status[]][],
);
const STATUS_LABEL = new Map<Status, string>(Object.entries(STATUS_LABELS) as [Status, string][]);
const STATUS_TONE = new Map<Status, string>(Object.entries(STATUS_TONES) as [Status, string][]);
const SEVERITY_LABEL = new Map<Severity, string>(Object.entries(SEVERITY_LABELS) as [Severity, string][]);
const SEVERITY_TONE = new Map<Severity, string>(Object.entries(SEVERITY_TONES) as [Severity, string][]);
const CAUSE_LABEL = new Map<CauseCode, string>(Object.entries(CAUSE_LABELS) as [CauseCode, string][]);

/** Every status, in lifecycle order. */
export const STATUSES = Object.keys(STATUS_LABELS) as readonly Status[];

/** Every severity, worst first — the order a queue is scanned in. */
export const SEVERITIES = Object.keys(SEVERITY_LABELS) as readonly Severity[];

/** The closed set of answers a resolution may carry. */
export const CAUSE_CODES = Object.keys(CAUSE_LABELS) as readonly CauseCode[];

/** Where a room standing at `from` may go next. Empty when it may go nowhere. */
export function allowedNextStatuses(from: Status): readonly Status[] {
  return NEXT.get(from) ?? [];
}

/** Whether a move to `to` must carry a cause code. */
export function requiresCause(to: Status): boolean {
  return to === 'resolved';
}

export function statusLabel(status: Status): string {
  return STATUS_LABEL.get(status) ?? status;
}

export function statusToneClass(status: Status): string {
  return STATUS_TONE.get(status) ?? 'bg-gray-700 text-gray-300';
}

export function severityLabel(severity: Severity): string {
  return SEVERITY_LABEL.get(severity) ?? severity;
}

export function severityToneClass(severity: Severity): string {
  return SEVERITY_TONE.get(severity) ?? 'bg-gray-700 text-gray-300';
}

export function causeLabel(cause: CauseCode): string {
  return CAUSE_LABEL.get(cause) ?? cause;
}
