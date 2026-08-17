import { render, screen, within } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AlertEvidencePanel } from './AlertEvidencePanel';
import type { components } from '../../types/api';

type AlertEvidence = components['schemas']['AlertEvidence'];

function evidence(over: Partial<AlertEvidence> = {}): AlertEvidence {
  return {
    ranked: [
      { dim: 'cpu.busy_pct', score: 0.94 },
      { dim: 'disk.await_ms', score: 0.41 },
    ],
    series: [
      { dim: 'cpu.busy_pct', points: [{ ts: 1, value: 40 }, { ts: 2, value: 96 }] },
      { dim: 'mem.used_pct', points: [{ ts: 1, value: 70 }, { ts: 2, value: 72 }] },
      { dim: 'disk.await_ms', points: [{ ts: 1, value: 4 }, { ts: 2, value: 190 }] },
    ],
    processes: [
      { rank: 1, basename: 'chrome', pid: 4242, cpu: 88.5, mem: 12.5 },
      { rank: 2, basename: 'postgres', pid: 900, cpu: 6.25, mem: 30 },
    ],
    log_samples: ['kernel: task nginx:1234 blocked for more than 120 seconds'],
    truncated: false,
    ...over,
  };
}

describe('AlertEvidencePanel — while it is being read', () => {
  it('says it is reading', () => {
    render(<AlertEvidencePanel evidence={undefined} loading error={undefined} />);
    expect(screen.getByText(/Reading the evidence/i)).toBeInTheDocument();
  });

  it('renders the server’s own words when an alert carries no evidence, not an error boundary', () => {
    render(<AlertEvidencePanel evidence={undefined} loading={false} error="the alert carries no evidence" />);
    expect(screen.getByRole('note')).toHaveTextContent('the alert carries no evidence');
  });

  it('renders the server’s own words when this build cannot read the codec', () => {
    render(<AlertEvidencePanel evidence={undefined} loading={false} error="evidence codec unknown to this build" />);
    expect(screen.getByRole('note')).toHaveTextContent('evidence codec unknown to this build');
  });
});

describe('AlertEvidencePanel — what the machine knew', () => {
  it('ranks the dimensions that broke pattern, worst first', () => {
    render(<AlertEvidencePanel evidence={evidence()} loading={false} error={undefined} />);
    const ranked = within(screen.getByRole('list', { name: /ranked dimensions/i })).getAllByRole('listitem');
    expect(ranked.at(0)).toHaveTextContent('cpu.busy_pct');
    expect(ranked.at(0)).toHaveTextContent('0.94');
    expect(ranked.at(1)).toHaveTextContent('disk.await_ms');
  });

  it('draws every series the evidence carries, at the resolution only the machine holds', () => {
    render(<AlertEvidencePanel evidence={evidence()} loading={false} error={undefined} />);
    expect(screen.getByRole('img', { name: /cpu\.busy_pct over the window/i })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: /mem\.used_pct over the window/i })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: /disk\.await_ms over the window/i })).toBeInTheDocument();
  });

  it('labels a series with where it started, ended and how far it ranged', () => {
    render(<AlertEvidencePanel evidence={evidence()} loading={false} error={undefined} />);
    const chart = screen.getByRole('img', { name: /cpu\.busy_pct over the window/i });
    expect(chart).toHaveAccessibleName(/40 to 96/);
  });

  it('renders a series too short to draw as its readings rather than as nothing', () => {
    render(
      <AlertEvidencePanel
        loading={false}
        error={undefined}
        evidence={evidence({ series: [{ dim: 'cpu.busy_pct', points: [{ ts: 1, value: 96 }] }] })}
      />,
    );
    expect(screen.getByText(/one reading: 96/i)).toBeInTheDocument();
  });

  it('lists the processes at the event instant', () => {
    render(<AlertEvidencePanel evidence={evidence()} loading={false} error={undefined} />);
    const row = within(screen.getByRole('table', { name: /processes/i })).getByRole('row', { name: /chrome/ });
    expect(within(row).getByText('4242')).toBeInTheDocument();
    expect(within(row).getByText('88.5%')).toBeInTheDocument();
    expect(within(row).getByText('12.5%')).toBeInTheDocument();
  });

  it('renders host log lines as text, never as markup', () => {
    const hostile = '<script>alert(1)</script> nginx died';
    render(<AlertEvidencePanel evidence={evidence({ log_samples: [hostile] })} loading={false} error={undefined} />);

    const logs = screen.getByRole('list', { name: /log lines/i });
    expect(within(logs).getByText(hostile)).toBeInTheDocument();
    expect(logs.querySelector('script')).toBeNull();
  });

  it('says a size cap cost this evidence something, so "nothing dropped" and "nobody checked" never look alike', () => {
    render(<AlertEvidencePanel evidence={evidence({ truncated: true })} loading={false} error={undefined} />);
    expect(screen.getByRole('alert')).toHaveTextContent(/size cap/i);
  });

  it('says nothing about truncation when nothing was dropped', () => {
    render(<AlertEvidencePanel evidence={evidence()} loading={false} error={undefined} />);
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('says which parts the machine recorded nothing for', () => {
    render(
      <AlertEvidencePanel
        loading={false}
        error={undefined}
        evidence={evidence({ ranked: [], series: [], processes: [], log_samples: [] })}
      />,
    );
    expect(screen.getByText(/No ranked dimensions/i)).toBeInTheDocument();
    expect(screen.getByText(/No series/i)).toBeInTheDocument();
    expect(screen.getByText(/No processes/i)).toBeInTheDocument();
    expect(screen.getByText(/No log lines/i)).toBeInTheDocument();
  });
});
