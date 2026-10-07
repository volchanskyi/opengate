import { render, screen, within } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { IncidentTimeline } from './IncidentTimeline';
import type { components } from '../../types/api';

type IncidentEvent = components['schemas']['IncidentEvent'];

function event(over: Partial<IncidentEvent> & Pick<IncidentEvent, 'id' | 'kind'>): IncidentEvent {
  return { at: '2026-08-12T09:14:00Z', body: {}, ...over };
}

describe('IncidentTimeline', () => {
  it('says the room has no history yet rather than rendering an empty box', () => {
    render(<IncidentTimeline events={[]} total={0} people={{}} />);
    expect(screen.getByText(/Nothing has happened in this room yet/i)).toBeInTheDocument();
  });

  it('keeps the order the server sent, which is the order it happened', () => {
    render(
      <IncidentTimeline
        people={{}}
        total={3}
        events={[
          event({ id: 'e1', kind: 'status_change', body: { from: 'new', to: 'acknowledged' } }),
          event({ id: 'e2', kind: 'comment', body: { body: 'checked the driver' } }),
          event({ id: 'e3', kind: 'resolution', body: { from: 'acknowledged', to: 'resolved', cause_code: 'fixed_by_tech' } }),
        ]}
      />,
    );

    const lines = screen.getAllByRole('listitem').map((li) => li.textContent ?? '');
    expect(lines.at(0)).toContain('New → Acknowledged');
    expect(lines.at(1)).toContain('Comment');
    expect(lines.at(2)).toContain('Resolved — Fixed by a technician');
  });

  it('carries a person’s own words through as text', () => {
    render(<IncidentTimeline people={{}} total={1} events={[event({ id: 'e1', kind: 'comment', body: { body: 'Driver rollout at 02:41' } })]} />);
    expect(screen.getByText('Driver rollout at 02:41')).toBeInTheDocument();
  });

  it('renders markup in a comment as the characters somebody typed, never as an element', () => {
    const hostile = '<img src=x onerror="alert(1)"> <b>bold</b>';
    render(<IncidentTimeline people={{}} total={1} events={[event({ id: 'e1', kind: 'comment', body: { body: hostile } })]} />);

    const line = screen.getByRole('listitem');
    expect(within(line).getByText(hostile)).toBeInTheDocument();
    expect(line.querySelector('img')).toBeNull();
    expect(line.querySelector('b')).toBeNull();
  });

  it('says the system acted when no person did', () => {
    render(
      <IncidentTimeline
        people={{}}
        total={1}
        events={[event({ id: 'e1', kind: 'resolution', body: { reason: 'no alert within the reopen window' } })]}
      />,
    );
    expect(screen.getByText(/by the system/i)).toBeInTheDocument();
  });

  it('names who acted when somebody did', () => {
    render(
      <IncidentTimeline
        people={{ '6f2b9c31-1111-2222-3333-444455556666': 'Dana Whitfield' }}
        total={1}
        events={[event({ id: 'e1', kind: 'comment', actor_id: '6f2b9c31-1111-2222-3333-444455556666', body: { body: 'hi' } })]}
      />,
    );
    expect(screen.getByText(/by Dana Whitfield/)).toBeInTheDocument();
  });

  it('names who a room was handed to', () => {
    render(
      <IncidentTimeline
        people={{ u1: 'Sam Okafor', u2: 'Dana Whitfield' }}
        total={1}
        events={[event({ id: 'e1', kind: 'assignment', actor_id: 'u1', body: { assignee_id: 'u2' } })]}
      />,
    );
    expect(screen.getByText('Assigned to Dana Whitfield')).toBeInTheDocument();
    expect(screen.getByText(/by Sam Okafor/)).toBeInTheDocument();
  });

  it('reads a person the room cannot name as a removed user, never as an id', () => {
    render(
      <IncidentTimeline
        people={{}}
        total={1}
        events={[event({ id: 'e1', kind: 'comment', actor_id: '6f2b9c31-1111-2222-3333-444455556666', body: { body: 'hi' } })]}
      />,
    );
    expect(screen.getByText(/by a removed user/)).toBeInTheDocument();
    expect(screen.queryByText(/6f2b9c31/)).toBeNull();
  });

  it('says what a bounded history is a bounded view of', () => {
    render(<IncidentTimeline people={{}} total={94} events={[event({ id: 'e1', kind: 'comment', body: { body: 'hi' } })]} />);
    expect(screen.getByText(/1 of 94/)).toBeInTheDocument();
  });

  it('says nothing about a count when the whole history is on screen', () => {
    render(<IncidentTimeline people={{}} total={1} events={[event({ id: 'e1', kind: 'comment', body: { body: 'hi' } })]} />);
    expect(screen.queryByText(/of 1/)).toBeNull();
  });
});
