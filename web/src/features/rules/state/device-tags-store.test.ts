import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../../lib/api';
import { useOrganizationStore } from '../../organizations';
import { useDeviceTagsStore } from './device-tags-store';

vi.mock('../../../lib/api', () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), DELETE: vi.fn() },
}));

const mockedGet = vi.mocked(api.GET);
const mockedPost = vi.mocked(api.POST);
const mockedPut = vi.mocked(api.PUT);
const mockedDelete = vi.mocked(api.DELETE);

const catalogue = () => ({
  data: {
    labels: [{ id: 'label-1', key: 'role', value: 'file-server', created_by: 'ivan' }],
    assignments: [{ device_id: 'fs01', tags: { role: 'file-server' } }],
  },
  error: undefined,
  response: { ok: true, status: 200 },
});

const noContent = () => ({ data: undefined, error: undefined, response: { ok: true, status: 204 } });
const inUse = () => ({
  data: undefined,
  error: { error: 'role=file-server is aimed at by 2 rule settings' },
  response: { ok: false, status: 409 },
});

function lastBody(mock: { mock: { calls: unknown[][] } }): unknown {
  const call = mock.mock.calls.at(-1) as unknown as [string, { body?: unknown }] | undefined;
  return call?.[1].body;
}

beforeEach(() => {
  vi.clearAllMocks();
  useOrganizationStore.setState({ selectedOrganizationId: null });
  useDeviceTagsStore.setState({ labels: [], assignments: [], isLoading: false, error: null });
});

describe('device-tags-store', () => {
  it('reads a customer\'s label list and who carries what', async () => {
    mockedGet.mockResolvedValue(catalogue() as never);
    await useDeviceTagsStore.getState().fetchTags();

    expect(useDeviceTagsStore.getState().labels).toHaveLength(1);
    expect(useDeviceTagsStore.getState().assignments[0]?.tags).toEqual({ role: 'file-server' });
  });

  it('adds a label and re-reads the list', async () => {
    mockedPost.mockResolvedValue(noContent() as never);
    mockedGet.mockResolvedValue(catalogue() as never);

    expect(await useDeviceTagsStore.getState().createLabel('env', 'production')).toBe(true);
    expect(lastBody(mockedPost)).toEqual({ key: 'env', value: 'production' });
    expect(mockedGet).toHaveBeenCalledTimes(1);
  });

  it('surfaces the refusal when a rule is aimed at the label being deleted', async () => {
    useDeviceTagsStore.setState({ labels: catalogue().data.labels });
    mockedDelete.mockResolvedValue(inUse() as never);

    expect(await useDeviceTagsStore.getState().deleteLabel('label-1')).toBe(false);
    expect(useDeviceTagsStore.getState().error).toContain('aimed at');
    expect(useDeviceTagsStore.getState().labels).toHaveLength(1);
    expect(mockedGet).not.toHaveBeenCalled();
  });

  it('gives one label to a set of machines at once', async () => {
    mockedPut.mockResolvedValue(noContent() as never);
    mockedGet.mockResolvedValue(catalogue() as never);

    expect(await useDeviceTagsStore.getState().assignLabel('label-1', ['fs01', 'fs02'])).toBe(true);
    expect(lastBody(mockedPut)).toEqual({ label_id: 'label-1', device_ids: ['fs01', 'fs02'] });
  });

  it('takes one label key off one machine', async () => {
    mockedDelete.mockResolvedValue(noContent() as never);
    mockedGet.mockResolvedValue(catalogue() as never);

    expect(await useDeviceTagsStore.getState().clearTag('fs01', 'role')).toBe(true);
    expect(mockedDelete).toHaveBeenCalledWith('/api/v1/device-tags/assignments', expect.anything());
  });
});
