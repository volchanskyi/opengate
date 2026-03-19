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

  it('fetchAmtDevices populates store', async () => {
    await useAMTStore.getState().fetchAmtDevices();
    expect(useAMTStore.getState().amtDevices).toHaveLength(1);
    expect(useAMTStore.getState().amtDevices[0]?.hostname).toBe('host-1');
  });

  it('sendPowerAction returns true on success', async () => {
    const result = await useAMTStore.getState().sendPowerAction('amt-1', 'power_on');
    expect(result).toBe(true);
  });

  it('sendPowerAction returns false on error', async () => {
    const { api } = await import('../lib/api');
    vi.mocked(api.POST).mockResolvedValueOnce({ data: undefined, error: { error: 'Device offline' } } as never);
    const result = await useAMTStore.getState().sendPowerAction('amt-1', 'power_on');
    expect(result).toBe(false);
  });
});
