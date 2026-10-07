import { act, fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../lib/api';
import { useOrganizationStore } from '../organizations';
import { useCatalogueStore } from '../rules';
import { InvestigationFilters } from './InvestigationFilters';
import { DEFAULT_QUEUE_FILTERS, type QueueFilters } from './state/queue-store';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn() } }));

const GET = vi.mocked(api.GET);

const filters = (over: Partial<QueueFilters> = {}): QueueFilters => ({ ...DEFAULT_QUEUE_FILTERS, ...over });

function rule(id: string) {
  return { id } as never;
}

function host(id: string, hostname: string, status: 'online' | 'offline') {
  return {
    id, hostname, status, organization_id: 'org-1', site_id: '', os: 'linux', agent_version: '',
    capabilities: [], last_seen: '', created_at: '', updated_at: '',
  };
}

beforeEach(() => {
  GET.mockReset();
  GET.mockResolvedValue({
    data: [host('d2', 'reception-pc', 'offline'), host('d1', 'backup-01', 'online')],
    response: { ok: true, status: 200 },
  } as never);
  useOrganizationStore.setState({ selectedOrganizationId: null, customerWanted: 0 });
  useCatalogueStore.setState({
    rules: [rule('memory-pressure'), rule('cpu-saturated')], fleetSize: 3, loaded: true, loading: false,
    error: null, fetchCatalogue: vi.fn().mockResolvedValue(undefined),
  });
});

describe('InvestigationFilters — status', () => {
  it('shows the one status the queue is narrowed to', () => {
    render(<InvestigationFilters filters={filters()} onChange={vi.fn()} />);

    const status = screen.getByRole('radiogroup', { name: 'Status' });
    expect(within(status).getAllByRole('radio').map((r) => r.textContent))
      .toEqual(['New', 'Acknowledged', 'Investigating', 'Resolved']);
    expect(screen.getByRole('radio', { name: 'New' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('radio', { name: 'Resolved' })).toHaveAttribute('aria-checked', 'false');
  });

  it('moves to another status in place of the one in force', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<InvestigationFilters filters={filters()} onChange={onChange} />);

    await user.click(screen.getByRole('radio', { name: 'Resolved' }));
    expect(onChange).toHaveBeenCalledWith({ status: 'resolved' });
  });
});

describe('InvestigationFilters — severity', () => {
  it('starts on every severity, shown as none of them pressed', () => {
    render(<InvestigationFilters filters={filters()} onChange={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Critical' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('narrows to one severity and composes with the status already set', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<InvestigationFilters filters={filters({ status: 'investigating' })} onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: 'Critical' }));
    expect(onChange).toHaveBeenCalledWith({ severity: ['critical'] });
    expect(screen.getByRole('radio', { name: 'Investigating' })).toHaveAttribute('aria-checked', 'true');
  });

  it('adds a second severity without dropping the first', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<InvestigationFilters filters={filters({ severity: ['critical'] })} onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: 'Warning' }));
    expect(onChange).toHaveBeenCalledWith({ severity: ['critical', 'warning'] });
  });
});

describe('InvestigationFilters — rule', () => {
  it('offers every catalogue rule after "All rules", whichever customer is chosen', () => {
    render(<InvestigationFilters filters={filters()} onChange={vi.fn()} />);

    const ruleList = screen.getByLabelText('Rule');
    expect(ruleList).toBeEnabled();
    expect(within(ruleList).getAllByRole('option').map((o) => o.textContent))
      .toEqual(['All rules', 'cpu-saturated', 'memory-pressure']);
  });

  it('applies a picked rule at once', () => {
    const onChange = vi.fn();
    render(<InvestigationFilters filters={filters()} onChange={onChange} />);

    fireEvent.change(screen.getByLabelText('Rule'), { target: { value: 'cpu-saturated' } });
    expect(onChange).toHaveBeenCalledWith({ ruleId: 'cpu-saturated' });
  });

  it('reads the catalogue when it has not been read yet', () => {
    const fetchCatalogue = vi.fn().mockResolvedValue(undefined);
    useCatalogueStore.setState({ rules: [], loaded: false, fetchCatalogue });
    render(<InvestigationFilters filters={filters()} onChange={vi.fn()} />);
    expect(fetchCatalogue).toHaveBeenCalled();
  });
});

describe('InvestigationFilters — host', () => {
  it('waits on a customer under "All customers" and asks for one', () => {
    render(<InvestigationFilters filters={filters()} onChange={vi.fn()} />);

    const control = screen.getByRole('button', { name: /Host/ });
    expect(control).toBeDisabled();
    expect(control).toHaveTextContent('Pick a customer first');
    expect(useOrganizationStore.getState().customerWanted).toBe(1);
  });

  it("offers the chosen customer's hosts by name and applies a pick at once", async () => {
    useOrganizationStore.setState({ selectedOrganizationId: 'org-1' });
    const onChange = vi.fn();
    render(<InvestigationFilters filters={filters()} onChange={onChange} />);

    const control = screen.getByRole('button', { name: /Host/ });
    expect(control).toHaveTextContent('All hosts');
    await vi.waitFor(() => { expect(GET).toHaveBeenCalled(); });
    await act(async () => { await Promise.resolve(); });

    fireEvent.click(control);
    const options = within(screen.getByRole('listbox', { name: 'Host' })).getAllByRole('option');
    expect(options.map((o) => o.textContent)).toEqual(['All hosts', 'backup-01online', 'reception-pcoffline']);

    fireEvent.click(options.at(2) ?? control);
    expect(onChange).toHaveBeenCalledWith({ deviceId: 'd2' });
  });
});

describe('InvestigationFilters — clearing', () => {
  it('goes back to the new incidents, every severity, every rule and every host', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(
      <InvestigationFilters
        filters={filters({ status: 'resolved', severity: ['critical'], ruleId: 'cpu-saturated', deviceId: 'd1' })}
        onChange={onChange}
      />,
    );

    await user.click(screen.getByRole('button', { name: 'Clear' }));
    expect(onChange).toHaveBeenCalledWith(DEFAULT_QUEUE_FILTERS);
  });

  it('has nothing left to apply, since every pick applies at once', () => {
    render(<InvestigationFilters filters={filters()} onChange={vi.fn()} />);
    expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull();
  });
});
