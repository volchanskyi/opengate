import { describe, it, expect } from 'vitest';
import {
  CAUSE_CODES,
  SEVERITIES,
  STATUSES,
  allowedNextStatuses,
  causeLabel,
  requiresCause,
  severityLabel,
  severityToneClass,
  statusLabel,
  statusToneClass,
} from './incident-lifecycle';

describe('allowedNextStatuses', () => {
  it('lets the queue be picked up, worked or closed in one move', () => {
    expect(allowedNextStatuses('new')).toEqual(['acknowledged', 'investigating', 'resolved']);
  });

  it('lets a picked-up room be handed back to the queue', () => {
    expect(allowedNextStatuses('acknowledged')).toEqual(['new', 'investigating', 'resolved']);
    expect(allowedNextStatuses('investigating')).toEqual(['new', 'acknowledged', 'resolved']);
  });

  it('gives a closed room no successor — an answer already given is not un-given here', () => {
    expect(allowedNextStatuses('resolved')).toEqual([]);
  });

  it('never offers a move to the status the room already stands in', () => {
    for (const from of STATUSES) {
      expect(allowedNextStatuses(from)).not.toContain(from);
    }
  });

  it('only ever names statuses the lifecycle knows', () => {
    for (const from of STATUSES) {
      for (const to of allowedNextStatuses(from)) {
        expect(STATUSES).toContain(to);
      }
    }
  });
});

describe('requiresCause', () => {
  it('demands a cause only when resolving', () => {
    expect(requiresCause('resolved')).toBe(true);
    expect(requiresCause('new')).toBe(false);
    expect(requiresCause('acknowledged')).toBe(false);
    expect(requiresCause('investigating')).toBe(false);
  });
});

describe('the closed cause-code set', () => {
  it('is exactly the seven the API accepts, in the order it declares them', () => {
    expect(CAUSE_CODES).toEqual([
      'resolved_self',
      'fixed_by_tech',
      'hardware_fault',
      'expected_load',
      'false_positive',
      'duplicate',
      'wont_fix',
    ]);
  });

  it('names each one in an operator’s words', () => {
    expect(causeLabel('resolved_self')).toBe('Resolved itself');
    expect(causeLabel('fixed_by_tech')).toBe('Fixed by a technician');
    expect(causeLabel('hardware_fault')).toBe('Hardware fault');
    expect(causeLabel('expected_load')).toBe('Expected load');
    expect(causeLabel('false_positive')).toBe('False alarm — the rule needs retuning');
    expect(causeLabel('duplicate')).toBe('Duplicate of another incident');
    expect(causeLabel('wont_fix')).toBe('Will not fix');
  });
});

describe('status vocabulary', () => {
  it('lists the four statuses in lifecycle order', () => {
    expect(STATUSES).toEqual(['new', 'acknowledged', 'investigating', 'resolved']);
  });

  it('labels each status', () => {
    expect(statusLabel('new')).toBe('New');
    expect(statusLabel('acknowledged')).toBe('Acknowledged');
    expect(statusLabel('investigating')).toBe('Investigating');
    expect(statusLabel('resolved')).toBe('Resolved');
  });

  it('gives every status a distinct tone', () => {
    const tones = STATUSES.map(statusToneClass);
    expect(new Set(tones).size).toBe(STATUSES.length);
  });
});

describe('severity vocabulary', () => {
  it('lists the three severities worst first', () => {
    expect(SEVERITIES).toEqual(['critical', 'warning', 'info']);
  });

  it('labels each severity', () => {
    expect(severityLabel('critical')).toBe('Critical');
    expect(severityLabel('warning')).toBe('Warning');
    expect(severityLabel('info')).toBe('Info');
  });

  it('gives every severity a distinct tone', () => {
    const tones = SEVERITIES.map(severityToneClass);
    expect(new Set(tones).size).toBe(SEVERITIES.length);
  });
});
