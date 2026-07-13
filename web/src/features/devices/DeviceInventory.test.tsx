import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useInventoryStore } from './state/inventory-store';
import { DeviceInventory } from './DeviceInventory';
import type { components } from '../../types/api';

type InventoryItem = components['schemas']['InventoryItem'];

function item(over: Partial<InventoryItem> & Pick<InventoryItem, 'kind' | 'name'>): InventoryItem {
  return {
    version: '', port: 0, proto: '', state: '', runtime: '', image: '',
    first_seen: '2026-07-01T00:00:00Z', last_seen: '2026-07-10T00:00:00Z', ...over,
  };
}

const items: InventoryItem[] = [
  item({ kind: 'port', name: 'sshd', port: 22, proto: 'tcp', state: 'LISTEN', last_seen: '2026-07-10T00:00:00Z' }),
  item({ kind: 'port', name: 'nginx', port: 443, proto: 'tcp', state: 'LISTEN', last_seen: '2026-07-11T00:00:00Z' }),
  item({ kind: 'service', name: 'cron.service', state: 'running' }),
  item({ kind: 'db_engine', name: 'postgres', version: '17.2', port: 5432 }),
  item({ kind: 'container', name: 'web', state: 'running', runtime: 'docker', image: 'nginx:latest' }),
  item({ kind: 'package', name: 'openssl', version: '3.0.2' }),
];

beforeEach(() => {
  vi.clearAllMocks();
  useInventoryStore.setState({ byDevice: new Map(), loading: new Map(), errors: new Map(), fetchInventory: vi.fn() });
});

describe('DeviceInventory', () => {
  it('fetches inventory on mount', () => {
    const fetchInventory = vi.fn();
    useInventoryStore.setState({ fetchInventory });
    render(<DeviceInventory deviceId="d1" />);
    expect(fetchInventory).toHaveBeenCalledWith('d1');
  });

  it('shows a loading state before data arrives', () => {
    useInventoryStore.setState({ loading: new Map([['d1', true]]) });
    render(<DeviceInventory deviceId="d1" />);
    expect(screen.getByText(/Loading inventory/i)).toBeInTheDocument();
  });

  it('shows an error state when loading failed and nothing is cached', () => {
    useInventoryStore.setState({ errors: new Map([['d1', 'Failed to load inventory.']]) });
    render(<DeviceInventory deviceId="d1" />);
    expect(screen.getByText('Failed to load inventory.')).toBeInTheDocument();
  });

  it('shows an empty state when the footprint is empty', () => {
    useInventoryStore.setState({ byDevice: new Map([['d1', []]]) });
    render(<DeviceInventory deviceId="d1" />);
    expect(screen.getByText(/No footprint discovered/i)).toBeInTheDocument();
  });

  it('renders grouped tables and a summary for a discovered footprint', () => {
    useInventoryStore.setState({ byDevice: new Map([['d1', items]]) });
    render(<DeviceInventory deviceId="d1" />);
    expect(screen.getByText('Listening Ports (2)')).toBeInTheDocument();
    expect(screen.getByText('Services (1)')).toBeInTheDocument();
    expect(screen.getByText('Database Engines (1)')).toBeInTheDocument();
    expect(screen.getByText('Containers (1)')).toBeInTheDocument();
    expect(screen.getByText('Packages (1)')).toBeInTheDocument();
    expect(screen.getByText('sshd')).toBeInTheDocument();
    expect(screen.getByText('nginx:latest')).toBeInTheDocument();
    expect(screen.getByText(/^Discovered:/)).toHaveTextContent('2 listening ports');
  });

  it('sorts a table when a column header is clicked', async () => {
    const user = userEvent.setup();
    useInventoryStore.setState({ byDevice: new Map([['d1', items]]) });
    render(<DeviceInventory deviceId="d1" />);
    const portsTable = screen.getByText('Listening Ports (2)').parentElement!.querySelector('table')!;
    const firstDataRow = () => within(portsTable).getAllByRole('row')[1];
    // Default sort: port ascending → 22 before 443.
    expect(firstDataRow()).toHaveTextContent('22');
    await user.click(within(portsTable).getByRole('button', { name: /^Port/ }));
    // Toggled to descending → 443 first.
    expect(firstDataRow()).toHaveTextContent('443');
  });

  it('force-refetches when Refresh is clicked', async () => {
    const user = userEvent.setup();
    const fetchInventory = vi.fn();
    useInventoryStore.setState({ byDevice: new Map([['d1', items]]), fetchInventory });
    render(<DeviceInventory deviceId="d1" />);
    await user.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(fetchInventory).toHaveBeenCalledWith('d1', true);
  });
});
