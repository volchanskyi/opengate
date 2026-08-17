import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { InvestigationFilters } from './InvestigationFilters';
import { DEFAULT_QUEUE_FILTERS, type QueueFilters } from './state/queue-store';

const filters = (over: Partial<QueueFilters> = {}): QueueFilters => ({ ...DEFAULT_QUEUE_FILTERS, ...over });

describe('InvestigationFilters — status', () => {
  it('shows which statuses the queue is narrowed to', () => {
    render(<InvestigationFilters filters={filters()} onChange={vi.fn()} />);

    expect(screen.getByRole('button', { name: 'New' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Resolved' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('adds a status without dropping the ones already on', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<InvestigationFilters filters={filters()} onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: 'Resolved' }));
    expect(onChange).toHaveBeenCalledWith({ status: ['new', 'acknowledged', 'investigating', 'resolved'] });
  });

  it('removes a status that was on', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<InvestigationFilters filters={filters()} onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: 'New' }));
    expect(onChange).toHaveBeenCalledWith({ status: ['acknowledged', 'investigating'] });
  });
});

describe('InvestigationFilters — severity', () => {
  it('starts on every severity, shown as none of them pressed', () => {
    render(<InvestigationFilters filters={filters()} onChange={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Critical' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('narrows to one severity and composes with the status filter already set', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<InvestigationFilters filters={filters({ status: ['investigating'] })} onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: 'Critical' }));
    expect(onChange).toHaveBeenCalledWith({ severity: ['critical'] });
    expect(screen.getByRole('button', { name: 'Investigating' })).toHaveAttribute('aria-pressed', 'true');
  });
});

describe('InvestigationFilters — rule and device', () => {
  it('applies both text filters in one change rather than one per keystroke', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<InvestigationFilters filters={filters()} onChange={onChange} />);

    await user.type(screen.getByLabelText('Rule'), 'cpu.sustained');
    await user.type(screen.getByLabelText('Device'), 'dev-7');
    expect(onChange).not.toHaveBeenCalled();

    await user.click(screen.getByRole('button', { name: 'Apply' }));
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({ ruleId: 'cpu.sustained', deviceId: 'dev-7' });
  });

  it('trims what was typed, so a stray space does not become a filter nothing matches', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<InvestigationFilters filters={filters()} onChange={onChange} />);

    await user.type(screen.getByLabelText('Rule'), '  cpu.sustained  ');
    await user.click(screen.getByRole('button', { name: 'Apply' }));
    expect(onChange).toHaveBeenCalledWith({ ruleId: 'cpu.sustained', deviceId: '' });
  });

  it('clears every narrowing back to the open queue', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<InvestigationFilters filters={filters({ severity: ['critical'], ruleId: 'cpu.sustained' })} onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: 'Clear' }));
    expect(onChange).toHaveBeenCalledWith(DEFAULT_QUEUE_FILTERS);
    expect(screen.getByLabelText('Rule')).toHaveValue('');
  });

  it('shows the filters it was given rather than an empty form', () => {
    render(<InvestigationFilters filters={filters({ ruleId: 'disk.await', deviceId: 'dev-3' })} onChange={vi.fn()} />);
    expect(screen.getByLabelText('Rule')).toHaveValue('disk.await');
    expect(screen.getByLabelText('Device')).toHaveValue('dev-3');
  });
});
