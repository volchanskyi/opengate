import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useInventoryStore } from './inventory-store';
import { api } from '../../../lib/api';
import type { components } from '../../../types/api';

vi.mock('../../../lib/api', () => ({
  api: { GET: vi.fn() },
}));

const mockedGet = vi.mocked(api.GET);

type InventoryItem = components['schemas']['InventoryItem'];

const sampleItems: InventoryItem[] = [
  {
    kind: 'port', name: 'nginx', version: '', port: 443, proto: 'tcp', state: 'LISTEN',
    runtime: '', image: '', first_seen: '2026-07-01T00:00:00Z', last_seen: '2026-07-10T00:00:00Z',
  },
];

function ok(items: InventoryItem[]) {
  return { data: { device_id: 'd1', items }, error: undefined, response: { ok: true, status: 200 } };
}

beforeEach(() => {
  vi.clearAllMocks();
  useInventoryStore.setState({ byDevice: new Map(), loading: new Map(), errors: new Map() });
});

describe('inventory-store', () => {
  it('caches items on success and clears loading', async () => {
    mockedGet.mockResolvedValue(ok(sampleItems) as never);
    await useInventoryStore.getState().fetchInventory('d1');
    expect(useInventoryStore.getState().byDevice.get('d1')).toEqual(sampleItems);
    expect(useInventoryStore.getState().loading.get('d1')).toBe(false);
    expect(useInventoryStore.getState().errors.get('d1')).toBeUndefined();
  });

  it('is cache-first: does not refetch when already cached', async () => {
    useInventoryStore.setState({ byDevice: new Map([['d1', sampleItems]]) });
    await useInventoryStore.getState().fetchInventory('d1');
    expect(mockedGet).not.toHaveBeenCalled();
  });

  it('force refetches even when cached', async () => {
    useInventoryStore.setState({ byDevice: new Map([['d1', []]]) });
    mockedGet.mockResolvedValue(ok(sampleItems) as never);
    await useInventoryStore.getState().fetchInventory('d1', true);
    expect(mockedGet).toHaveBeenCalledTimes(1);
    expect(useInventoryStore.getState().byDevice.get('d1')).toEqual(sampleItems);
  });

  it('records an error and caches nothing when the request fails', async () => {
    mockedGet.mockResolvedValue({ data: undefined, error: { error: 'boom' }, response: { ok: false, status: 500 } } as never);
    await useInventoryStore.getState().fetchInventory('d1');
    expect(useInventoryStore.getState().byDevice.get('d1')).toBeUndefined();
    expect(useInventoryStore.getState().errors.get('d1')).toBeTruthy();
    expect(useInventoryStore.getState().loading.get('d1')).toBe(false);
  });

  it('skips a concurrent fetch while one is already in flight', async () => {
    let resolve!: (v: unknown) => void;
    mockedGet.mockReturnValue(new Promise((r) => { resolve = r; }) as never);
    const p1 = useInventoryStore.getState().fetchInventory('d1');
    const p2 = useInventoryStore.getState().fetchInventory('d1');
    resolve(ok(sampleItems));
    await Promise.all([p1, p2]);
    expect(mockedGet).toHaveBeenCalledTimes(1);
  });
});
