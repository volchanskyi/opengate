import { describe, it, expect } from 'vitest';
import type { components } from '../../types/api';
import {
  alertReadingLabel,
  countLabel,
  durationLabel,
  eventLine,
  formatMoment,
  personName,
  scopeLabel,
} from './incident-format';

type IncidentEvent = components['schemas']['IncidentEvent'];
type Incident = components['schemas']['Incident'];

function event(over: Partial<IncidentEvent> & Pick<IncidentEvent, 'kind'>): IncidentEvent {
  return { id: 'e1', at: '2026-08-12T09:14:00Z', body: {}, ...over };
}

describe('formatMoment', () => {
  it('renders a real instant', () => {
    expect(formatMoment('2026-08-12T09:14:00Z')).toContain('2026');
  });

  it('renders an unparseable instant as a dash rather than "Invalid Date"', () => {
    expect(formatMoment('nonsense')).toBe('—');
    expect(formatMoment('')).toBe('—');
    expect(formatMoment(undefined)).toBe('—');
  });
});

describe('durationLabel', () => {
  const base = '2026-08-12T09:00:00Z';
  const after = (ms: number) => new Date(Date.parse(base) + ms).toISOString();

  it('says "under a minute" rather than "0 m"', () => {
    expect(durationLabel(base, after(30_000))).toBe('under a minute');
  });

  it('renders minutes on their own below an hour', () => {
    expect(durationLabel(base, after(45 * 60_000))).toBe('45 m');
  });

  it('renders hours and minutes below a day', () => {
    expect(durationLabel(base, after(2 * 3_600_000 + 5 * 60_000))).toBe('2 h 5 m');
  });

  it('drops a zero minute component', () => {
    expect(durationLabel(base, after(3 * 3_600_000))).toBe('3 h');
  });

  it('renders days and hours above a day', () => {
    expect(durationLabel(base, after(2 * 86_400_000 + 4 * 3_600_000))).toBe('2 d 4 h');
  });

  it('drops a zero hour component', () => {
    expect(durationLabel(base, after(5 * 86_400_000))).toBe('5 d');
  });

  it('treats a backwards span as no time at all rather than a negative one', () => {
    expect(durationLabel(after(60_000), base)).toBe('under a minute');
  });

  it('renders an unparseable span as a dash', () => {
    expect(durationLabel('nonsense', base)).toBe('—');
    expect(durationLabel(base, '')).toBe('—');
  });
});

describe('countLabel', () => {
  it('keeps the singular at one', () => {
    expect(countLabel(1, 'alert', 'alerts')).toBe('1 alert');
  });

  it('uses the plural at every other count, zero included', () => {
    expect(countLabel(0, 'alert', 'alerts')).toBe('0 alerts');
    expect(countLabel(312, 'alert', 'alerts')).toBe('312 alerts');
  });
});

describe('alertReadingLabel', () => {
  it('renders the metric and its reading', () => {
    expect(alertReadingLabel('cpu.busy_pct', 96.4)).toBe('cpu.busy_pct 96.4');
  });

  it('trims a reading to two decimals and drops trailing zeros', () => {
    expect(alertReadingLabel('disk.await_ms', 12.3456)).toBe('disk.await_ms 12.35');
    expect(alertReadingLabel('mem.used_pct', 80)).toBe('mem.used_pct 80');
  });

  it('renders the metric alone when the rule recorded no reading', () => {
    expect(alertReadingLabel('stall.io_pct', null)).toBe('stall.io_pct');
    expect(alertReadingLabel('stall.io_pct', undefined)).toBe('stall.io_pct');
  });

  it('renders a dash for a rule that fires on an event rather than a reading', () => {
    expect(alertReadingLabel('', 1)).toBe('—');
    expect(alertReadingLabel(undefined, null)).toBe('—');
  });
});

describe('eventLine — a handover reads the timeline', () => {
  it('names both ends of a status move, because "acknowledged" alone does not say which way', () => {
    const line = eventLine(event({ kind: 'status_change', body: { from: 'new', to: 'acknowledged' } }));
    expect(line.title).toBe('New → Acknowledged');
    expect(line.quote).toBe('');
  });

  it('marks the one move that withdraws an answer already given', () => {
    const line = eventLine(event({ kind: 'status_change', body: { from: 'resolved', to: 'new', reopened: true } }));
    expect(line.title).toBe('Reopened — Resolved → New');
  });

  it('says which answer closed the room', () => {
    const line = eventLine(event({ kind: 'resolution', body: { from: 'investigating', to: 'resolved', cause_code: 'false_positive' } }));
    expect(line.title).toBe('Resolved — False alarm — the rule needs retuning');
  });

  it('tells an auto-resolution apart from somebody’s decision', () => {
    const line = eventLine(event({ kind: 'resolution', body: { reason: 'no alert within the reopen window' } }));
    expect(line.title).toBe('Resolved automatically — no alert within the reopen window');
  });

  it('falls back to a bare resolution when the body says nothing', () => {
    expect(eventLine(event({ kind: 'resolution' })).title).toBe('Resolved');
  });

  it('names who a room was handed to, and says when it was handed back', () => {
    const people = { '6f2b9c31-1111-2222-3333-444455556666': 'Dana Whitfield' };
    const assigned = eventLine(event({ kind: 'assignment', body: { assignee_id: '6f2b9c31-1111-2222-3333-444455556666' } }), people);
    expect(assigned.title).toBe('Assigned to Dana Whitfield');
    expect(eventLine(event({ kind: 'assignment', body: { unassigned: true } })).title).toBe('Unassigned');
  });

  it('never shows an id for somebody the room cannot name', () => {
    const assigned = eventLine(event({ kind: 'assignment', body: { assignee_id: '6f2b9c31-1111-2222-3333-444455556666' } }));
    expect(assigned.title).toBe('Assigned to a removed user');
  });

  it('carries a comment’s words in the quote, never in the title', () => {
    const line = eventLine(event({ kind: 'comment', body: { body: 'Driver rollout at 02:41 — rolled back' } }));
    expect(line.title).toBe('Comment');
    expect(line.quote).toBe('Driver rollout at 02:41 — rolled back');
  });

  it('names the two kinds the room records for it rather than the incident', () => {
    expect(eventLine(event({ kind: 'alert_folded' })).title).toBe('Alert folded in');
    expect(eventLine(event({ kind: 'device_offline' })).title).toBe('Host went offline');
  });

  it('ignores a body field of the wrong type instead of rendering "[object Object]"', () => {
    const line = eventLine(event({ kind: 'comment', body: { body: { nested: true } } }));
    expect(line.quote).toBe('');
  });

  it('falls back to a bare status change when the body carries no ends', () => {
    expect(eventLine(event({ kind: 'status_change' })).title).toBe('Status changed');
  });
});

describe('personName', () => {
  const people = { u1: 'Dana Whitfield', u2: '  ' };

  it('names a colleague by the name they go by', () => {
    expect(personName(people, 'u1')).toBe('Dana Whitfield');
  });

  it('reads an id the room could not name as a removed user', () => {
    expect(personName(people, 'u9')).toBe('a removed user');
  });

  it('says a colleague has no name rather than printing a blank', () => {
    expect(personName(people, 'u2')).toBe('a user with no name');
  });
});

describe('scopeLabel', () => {
  function about(scope: Incident['scope'], scopeName: string | null): Pick<Incident, 'scope' | 'scope_name'> {
    return { scope, scope_name: scopeName };
  }

  it('names what a room is about in the product words', () => {
    expect(scopeLabel(about('device', 'reception-pc'))).toBe('Host · reception-pc');
    expect(scopeLabel(about('site', 'Front Desk'))).toBe('Site · Front Desk');
    expect(scopeLabel(about('organization', 'Acme Dental'))).toBe('Customer · Acme Dental');
  });

  it('says the record is gone rather than showing its id', () => {
    expect(scopeLabel(about('device', null))).toBe('Host · a removed host');
    expect(scopeLabel(about('site', null))).toBe('Site · a removed site');
    expect(scopeLabel(about('organization', null))).toBe('Customer · a removed customer');
  });
});
