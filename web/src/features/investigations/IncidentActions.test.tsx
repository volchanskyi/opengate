import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { IncidentActions } from './IncidentActions';
import { useRoomStore } from './state/room-store';
import { useAuthStore } from '../../state/auth-store';
import { CAUSE_CODES, causeLabel } from './incident-lifecycle';
import type { components } from '../../types/api';

type Incident = components['schemas']['Incident'];

function incident(over: Partial<Incident> = {}): Incident {
  return {
    id: 'i1', organization_id: 'org-1', rule_id: 'cpu.sustained', scope: 'organization',
    scope_key: 'org-1', severity: 'critical', status: 'new',
    opened_at: '2026-08-12T09:00:00Z', first_seen: '2026-08-12T09:00:00Z',
    last_seen: '2026-08-12T11:05:00Z', occurrences: 312, device_count: 40, ...over,
  };
}

const setStatus = vi.fn().mockResolvedValue(true);
const setAssignee = vi.fn().mockResolvedValue(true);
const addComment = vi.fn().mockResolvedValue(true);

beforeEach(() => {
  vi.clearAllMocks();
  setStatus.mockResolvedValue(true);
  setAssignee.mockResolvedValue(true);
  addComment.mockResolvedValue(true);
  useAuthStore.setState({ user: { id: 'user-3', email: 'tech@example.com', is_admin: false } as never });
  useRoomStore.setState({ acting: false, actionError: null, setStatus, setAssignee, addComment });
});

describe('IncidentActions — an illegal move is not offerable', () => {
  it('offers every move the lifecycle allows out of the queue', () => {
    render(<IncidentActions incident={incident({ status: 'new' })} />);
    expect(screen.getByRole('button', { name: 'Acknowledged' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Investigating' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Resolve' })).toBeInTheDocument();
  });

  it('never offers a move to where the room already stands', () => {
    render(<IncidentActions incident={incident({ status: 'acknowledged' })} />);
    expect(screen.queryByRole('button', { name: 'Acknowledged' })).toBeNull();
    expect(screen.getByRole('button', { name: 'New' })).toBeInTheDocument();
  });

  it('offers a closed room no move at all — an answer given is not un-given here', () => {
    render(<IncidentActions incident={incident({ status: 'resolved', cause_code: 'fixed_by_tech' })} />);
    expect(screen.queryByRole('button', { name: 'New' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Resolve' })).toBeNull();
    expect(screen.getByText(/Resolved — Fixed by a technician/i)).toBeInTheDocument();
  });

  it('sends a move the moment it is chosen', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'new' })} />);

    await user.click(screen.getByRole('button', { name: 'Investigating' }));
    expect(setStatus).toHaveBeenCalledWith('i1', 'investigating');
  });
});

describe('IncidentActions — resolving needs an answer', () => {
  it('offers exactly the closed set of cause codes and nothing else', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'investigating' })} />);
    await user.click(screen.getByRole('button', { name: 'Resolve' }));

    const options = [...screen.getByLabelText('Why it ended').querySelectorAll('option')]
      .map((o) => o.value)
      .filter((v) => v !== '');
    expect(options).toEqual([...CAUSE_CODES]);
  });

  it('names every cause in an operator’s words', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'investigating' })} />);
    await user.click(screen.getByRole('button', { name: 'Resolve' }));

    for (const code of CAUSE_CODES) {
      expect(screen.getByRole('option', { name: causeLabel(code) })).toBeInTheDocument();
    }
  });

  it('will not resolve until a cause is chosen', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'investigating' })} />);
    await user.click(screen.getByRole('button', { name: 'Resolve' }));

    expect(screen.getByRole('button', { name: 'Confirm resolution' })).toBeDisabled();
    expect(setStatus).not.toHaveBeenCalled();
  });

  it('resolves with the cause that was chosen', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'investigating' })} />);
    await user.click(screen.getByRole('button', { name: 'Resolve' }));
    await user.selectOptions(screen.getByLabelText('Why it ended'), 'false_positive');
    await user.click(screen.getByRole('button', { name: 'Confirm resolution' }));

    expect(setStatus).toHaveBeenCalledWith('i1', 'resolved', 'false_positive');
  });

  it('never carries a cause on a move that is not a resolution', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'new' })} />);
    await user.click(screen.getByRole('button', { name: 'Resolve' }));
    await user.selectOptions(screen.getByLabelText('Why it ended'), 'duplicate');
    await user.click(screen.getByRole('button', { name: 'Acknowledged' }));

    expect(setStatus).toHaveBeenCalledWith('i1', 'acknowledged');
  });

  it('puts the resolution form away when it is abandoned', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'new' })} />);
    await user.click(screen.getByRole('button', { name: 'Resolve' }));
    await user.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(screen.queryByLabelText('Why it ended')).toBeNull();
  });
});

describe('IncidentActions — a refused move', () => {
  it('surfaces the server’s words rather than a message of its own', async () => {
    setStatus.mockResolvedValue(false);
    useRoomStore.setState({ actionError: 'illegal incident transition: resolved to new' });
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'new' })} />);

    await user.click(screen.getByRole('button', { name: 'Acknowledged' }));
    expect(screen.getByRole('alert')).toHaveTextContent('illegal incident transition: resolved to new');
  });

  it('keeps the resolution form open after a refusal, so the answer is not retyped', async () => {
    setStatus.mockResolvedValue(false);
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'investigating' })} />);

    await user.click(screen.getByRole('button', { name: 'Resolve' }));
    await user.selectOptions(screen.getByLabelText('Why it ended'), 'duplicate');
    await user.click(screen.getByRole('button', { name: 'Confirm resolution' }));

    expect(screen.getByLabelText('Why it ended')).toHaveValue('duplicate');
  });

  it('puts the form away once the resolution is accepted', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ status: 'investigating' })} />);

    await user.click(screen.getByRole('button', { name: 'Resolve' }));
    await user.selectOptions(screen.getByLabelText('Why it ended'), 'duplicate');
    await user.click(screen.getByRole('button', { name: 'Confirm resolution' }));

    expect(screen.queryByLabelText('Why it ended')).toBeNull();
  });
});

describe('IncidentActions — who is working it', () => {
  it('offers to take an unheld room', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident()} />);

    await user.click(screen.getByRole('button', { name: /Take it/i }));
    expect(setAssignee).toHaveBeenCalledWith('i1', 'user-3');
  });

  it('offers to hand back the room this person holds', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ assignee_id: 'user-3' })} />);

    await user.click(screen.getByRole('button', { name: /Hand it back/i }));
    expect(setAssignee).toHaveBeenCalledWith('i1', null);
  });

  it('says who holds a room somebody else is working, and offers to take it over', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident({ assignee_id: 'aaaaaaaa-2222-3333-4444-555566667777' })} />);

    expect(screen.getByText(/aaaaaaaa/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /Take it over/i }));
    expect(setAssignee).toHaveBeenCalledWith('i1', 'user-3');
  });
});

describe('IncidentActions — notes', () => {
  it('adds a note and empties the box', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident()} />);

    await user.type(screen.getByLabelText('Add a note'), 'rolled the driver back');
    await user.click(screen.getByRole('button', { name: 'Add note' }));

    expect(addComment).toHaveBeenCalledWith('i1', 'rolled the driver back');
    expect(screen.getByLabelText('Add a note')).toHaveValue('');
  });

  it('will not send a note that says nothing', async () => {
    const user = userEvent.setup();
    render(<IncidentActions incident={incident()} />);

    expect(screen.getByRole('button', { name: 'Add note' })).toBeDisabled();
    await user.type(screen.getByLabelText('Add a note'), '   ');
    expect(screen.getByRole('button', { name: 'Add note' })).toBeDisabled();
  });

  it('keeps a refused note in the box rather than throwing away what was typed', async () => {
    addComment.mockResolvedValue(false);
    const user = userEvent.setup();
    render(<IncidentActions incident={incident()} />);

    await user.type(screen.getByLabelText('Add a note'), 'rolled the driver back');
    await user.click(screen.getByRole('button', { name: 'Add note' }));
    expect(screen.getByLabelText('Add a note')).toHaveValue('rolled the driver back');
  });

  it('lets a closed room still be annotated — a handover outlives the answer', () => {
    render(<IncidentActions incident={incident({ status: 'resolved', cause_code: 'duplicate' })} />);
    expect(screen.getByLabelText('Add a note')).toBeInTheDocument();
  });
});

describe('IncidentActions — while a move is in flight', () => {
  it('stops a second one being sent on top of it', () => {
    useRoomStore.setState({ acting: true });
    render(<IncidentActions incident={incident()} />);
    expect(screen.getByRole('button', { name: 'Acknowledged' })).toBeDisabled();
  });

  it('offers no remediation from the room — no restart, no script, no session', () => {
    render(<IncidentActions incident={incident()} />);
    for (const name of [/restart/i, /run script/i, /isolate/i, /start session/i]) {
      expect(screen.queryByRole('button', { name })).toBeNull();
    }
  });
});
