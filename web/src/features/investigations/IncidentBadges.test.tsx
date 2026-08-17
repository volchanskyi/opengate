import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { IncidentSeverityBadge, IncidentStatusBadge } from './IncidentBadges';
import { SEVERITIES, STATUSES } from './incident-lifecycle';

describe('IncidentSeverityBadge', () => {
  it('says how bad it is in words, not only in colour', () => {
    render(<IncidentSeverityBadge severity="critical" />);
    expect(screen.getByText('Critical')).toBeInTheDocument();
  });

  it('renders every severity the API can send', () => {
    render(<>{SEVERITIES.map((s) => <IncidentSeverityBadge key={s} severity={s} />)}</>);
    expect(screen.getByText('Critical')).toBeInTheDocument();
    expect(screen.getByText('Warning')).toBeInTheDocument();
    expect(screen.getByText('Info')).toBeInTheDocument();
  });
});

describe('IncidentStatusBadge', () => {
  it('says where the room stands', () => {
    render(<IncidentStatusBadge status="investigating" />);
    expect(screen.getByText('Investigating')).toBeInTheDocument();
  });

  it('renders every status the lifecycle carries', () => {
    render(<>{STATUSES.map((s) => <IncidentStatusBadge key={s} status={s} />)}</>);
    expect(screen.getByText('New')).toBeInTheDocument();
    expect(screen.getByText('Acknowledged')).toBeInTheDocument();
    expect(screen.getByText('Investigating')).toBeInTheDocument();
    expect(screen.getByText('Resolved')).toBeInTheDocument();
  });
});
