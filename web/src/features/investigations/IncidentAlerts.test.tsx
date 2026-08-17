import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { IncidentAlerts } from './IncidentAlerts';
import { useRoomStore } from './state/room-store';
import type { components } from '../../types/api';

type IncidentAlert = components['schemas']['IncidentAlert'];
type AlertEvidence = components['schemas']['AlertEvidence'];

function alert(over: Partial<IncidentAlert> = {}): IncidentAlert {
  return {
    id: 'a1', device_id: '6f2b9c31-1111-2222-3333-444455556666', rule_id: 'cpu.sustained',
    rule_version: 3, severity: 'critical', metric: 'cpu.busy_pct', value: 96.4,
    window_start: '2026-08-12T09:00:00Z', window_end: '2026-08-12T09:01:00Z',
    observed_at: '2026-08-12T09:00:30Z', received_at: '2026-08-12T09:00:45Z',
    backfilled: false, evidence_bytes: 4096, ...over,
  };
}

const evidence: AlertEvidence = {
  ranked: [{ dim: 'cpu.busy_pct', score: 0.94 }],
  series: [], processes: [], log_samples: [], truncated: false,
};

function renderAlerts(props: Partial<React.ComponentProps<typeof IncidentAlerts>> = {}) {
  const router = createMemoryRouter(
    [
      { path: '/', element: <IncidentAlerts incidentId="i1" alerts={[alert()]} total={1} deviceCount={1} {...props} /> },
      { path: '/devices/:id', element: <p>Device</p> },
    ],
    { initialEntries: ['/'] },
  );
  return render(<RouterProvider router={router} />);
}

beforeEach(() => {
  useRoomStore.setState({
    evidence: new Map(), evidenceLoading: new Map(), evidenceErrors: new Map(),
    fetchEvidence: vi.fn().mockResolvedValue(undefined),
  });
});

describe('IncidentAlerts — what each alert says', () => {
  it('names the machine and links to it without asking anything of it', () => {
    renderAlerts();
    const link = screen.getByRole('link', { name: '6f2b9c31' });
    expect(link).toHaveAttribute('href', '/devices/6f2b9c31-1111-2222-3333-444455556666');
  });

  it('shows what crossed the line and when', () => {
    renderAlerts();
    const row = screen.getByRole('row', { name: /6f2b9c31/ });
    expect(within(row).getByText('cpu.busy_pct 96.4')).toBeInTheDocument();
    expect(within(row).getByText('Critical')).toBeInTheDocument();
  });

  it('marks a finding a retroactive scan produced over local history', () => {
    renderAlerts({ alerts: [alert({ backfilled: true })] });
    expect(screen.getByText('Backfilled')).toBeInTheDocument();
  });

  it('does not mark an ordinary live alert', () => {
    renderAlerts();
    expect(screen.queryByText('Backfilled')).toBeNull();
  });

  it('says what fetching the evidence costs before anybody fetches it', () => {
    renderAlerts();
    expect(screen.getByRole('button', { name: /Show evidence/i })).toHaveTextContent('4.0 KB');
  });

  it('offers nothing to open for an alert that carries no evidence', () => {
    renderAlerts({ alerts: [alert({ evidence_bytes: 0 })] });
    expect(screen.queryByRole('button', { name: /Show evidence/i })).toBeNull();
    expect(screen.getByText(/No evidence was recorded/i)).toBeInTheDocument();
  });
});

describe('IncidentAlerts — opening evidence', () => {
  it('fetches an alert’s evidence only when it is opened', async () => {
    const fetchEvidence = vi.fn().mockResolvedValue(undefined);
    useRoomStore.setState({ fetchEvidence });
    const user = userEvent.setup();
    renderAlerts();

    expect(fetchEvidence).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: /Show evidence/i }));
    expect(fetchEvidence).toHaveBeenCalledWith('i1', 'a1');
  });

  it('renders the evidence the room already holds', async () => {
    useRoomStore.setState({ evidence: new Map([['a1', evidence]]) });
    const user = userEvent.setup();
    renderAlerts();

    await user.click(screen.getByRole('button', { name: /Show evidence/i }));
    expect(screen.getByRole('list', { name: /ranked dimensions/i })).toBeInTheDocument();
  });

  it('closes again without losing what was fetched', async () => {
    useRoomStore.setState({ evidence: new Map([['a1', evidence]]) });
    const user = userEvent.setup();
    renderAlerts();

    await user.click(screen.getByRole('button', { name: /Show evidence/i }));
    await user.click(screen.getByRole('button', { name: /Hide evidence/i }));
    expect(screen.queryByRole('list', { name: /ranked dimensions/i })).toBeNull();
  });

  it('opens two alerts at once, because comparing them is the point', async () => {
    useRoomStore.setState({ evidence: new Map([['a1', evidence], ['a2', evidence]]) });
    const user = userEvent.setup();
    renderAlerts({ alerts: [alert({ id: 'a1' }), alert({ id: 'a2', device_id: 'aaaaaaaa-2222-3333-4444-555566667777' })], total: 2, deviceCount: 2 });

    for (const button of screen.getAllByRole('button', { name: /Show evidence/i })) {
      await user.click(button);
    }
    expect(screen.getAllByRole('list', { name: /ranked dimensions/i })).toHaveLength(2);
  });

  it('surfaces an evidence failure against the alert it belongs to', async () => {
    useRoomStore.setState({ evidenceErrors: new Map([['a1', 'the alert carries no evidence']]) });
    const user = userEvent.setup();
    renderAlerts();

    await user.click(screen.getByRole('button', { name: /Show evidence/i }));
    expect(screen.getByRole('note')).toHaveTextContent('the alert carries no evidence');
  });
});

describe('IncidentAlerts — an incident wider than the alerts on screen', () => {
  it('says what a bounded page is a page of', () => {
    renderAlerts({ alerts: [alert()], total: 312, deviceCount: 40 });
    expect(screen.getByText(/1 of 312/)).toBeInTheDocument();
  });

  it('says how many machines are on screen against how many the incident covers', () => {
    renderAlerts({ alerts: [alert()], total: 2, deviceCount: 3 });
    expect(screen.getByText(/1 of 3 machines/)).toBeInTheDocument();
  });

  it('says nothing about the counts when the whole incident is on screen', () => {
    renderAlerts({ alerts: [alert()], total: 1, deviceCount: 1 });
    expect(screen.queryByText(/of 1 machine/)).toBeNull();
  });

  it('renders a room holding no alerts as a room, not as an error', () => {
    renderAlerts({ alerts: [], total: 0, deviceCount: 0 });
    expect(screen.getByText(/No alerts are held in this room/i)).toBeInTheDocument();
    expect(screen.queryByRole('table')).toBeNull();
  });
});
