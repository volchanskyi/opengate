import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAMTStore } from './amt-store';

vi.mock('../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({
      data: [
        { uuid: 'amt-1', hostname: 'host-1', model: 'vPro', firmware: '16.0', status: 'online', last_seen: '2026-01-01T00:00:00Z' },
      ],
      error: undefined,
    }),
    POST: vi.fn().mockResolvedValue({ data: {}, error: undefined }),
  },
}));

describe('AMTStore', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAMTStore.setState({
      amtDevices: [],
      selectedAmtDevice: null,
      isLoading: false,
      error: null,
    });
  });

  it('initial state', () => {
    const fresh = useAMTStore.getState();
    expect(fresh.amtDevices).toEqual([]);
    expect(fresh.selectedAmtDevice).toBeNull();
    expect(fresh.isLoading).toBe(false);
    expect(fresh.error).toBeNull();
  });

  it('fetchAmtDevices populates store', async () => {
    await useAMTStore.getState().fetchAmtDevices();
    expect(useAMTStore.getState().amtDevices).toHaveLength(1);
    expect(useAMTStore.getState().amtDevices[0]?.hostname).toBe('host-1');
  });

  it('fetchAmtDevice populates selectedAmtDevice via path param', async () => {
    const { api } = await import('../lib/api');
    vi.mocked(api.GET).mockResolvedValueOnce({
      data: { uuid: 'amt-7', hostname: 'h7', model: 'vPro', firmware: '16.0', status: 'online', last_seen: '' },
      error: undefined,
    } as never);

    await useAMTStore.getState().fetchAmtDevice('amt-7');

    expect(useAMTStore.getState().selectedAmtDevice?.uuid).toBe('amt-7');
    expect(api.GET).toHaveBeenCalledWith('/api/v1/amt/devices/{uuid}', {
      params: { path: { uuid: 'amt-7' } },
    });
  });

  it('fetchAmtDevice on error keeps selectedAmtDevice unchanged', async () => {
    const { api } = await import('../lib/api');
    useAMTStore.setState({ selectedAmtDevice: null });
    vi.mocked(api.GET).mockResolvedValueOnce({ data: undefined, error: { error: 'gone' } } as never);

    await useAMTStore.getState().fetchAmtDevice('amt-9');

    // Kills `if (res.ok)` → `if (true)` mutant on fetchAmtDevice.
    expect(useAMTStore.getState().selectedAmtDevice).toBeNull();
  });

  it('sendPowerAction returns true on success and sends action body', async () => {
    const { api } = await import('../lib/api');
    vi.mocked(api.POST).mockResolvedValueOnce({ data: {}, error: undefined } as never);

    const result = await useAMTStore.getState().sendPowerAction('amt-1', 'power_on');

    expect(result).toBe(true);
    expect(api.POST).toHaveBeenCalledWith('/api/v1/amt/devices/{uuid}/power', {
      params: { path: { uuid: 'amt-1' } },
      body: { action: 'power_on' },
    });
  });

  it('sendPowerAction returns false on error', async () => {
    const { api } = await import('../lib/api');
    vi.mocked(api.POST).mockResolvedValueOnce({ data: undefined, error: { error: 'Device offline' } } as never);
    const result = await useAMTStore.getState().sendPowerAction('amt-1', 'power_on');
    expect(result).toBe(false);
  });
});
