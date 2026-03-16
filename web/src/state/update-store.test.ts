import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useUpdateStore } from './update-store';

const mockGet = vi.fn();
const mockPost = vi.fn();
const mockDelete = vi.fn();

vi.mock('../lib/api', () => ({
  api: {
    GET: (...args: unknown[]) => mockGet(...args),
    POST: (...args: unknown[]) => mockPost(...args),
    DELETE: (...args: unknown[]) => mockDelete(...args),
  },
}));

const fakeManifest = {
  version: '1.0.0',
  os: 'linux',
  arch: 'amd64',
  url: 'https://example.com/agent',
  sha256: 'abc123',
  signature: 'sig',
  created_at: '2024-01-01T00:00:00Z',
};

const fakeToken = {
  id: 't1',
  token: 'abcdef1234567890',
  label: 'test-token',
  created_by: 'u1',
  max_uses: 5,
  use_count: 0,
  expires_at: '2024-01-02T00:00:00Z',
  created_at: '2024-01-01T00:00:00Z',
};

describe('update store', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useUpdateStore.setState({
      manifests: [],
      enrollmentTokens: [],
      caPem: null,
      isLoading: false,
      error: null,
    });
  });

  it('fetchManifests populates manifests array', async () => {
    mockGet.mockResolvedValueOnce({ data: [fakeManifest], error: undefined });

    await useUpdateStore.getState().fetchManifests();

    expect(useUpdateStore.getState().manifests).toEqual([fakeManifest]);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/updates/manifests');
  });

  it('fetchManifests handles error', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    await useUpdateStore.getState().fetchManifests();

    expect(useUpdateStore.getState().error).toBe('forbidden');
    expect(useUpdateStore.getState().manifests).toEqual([]);
  });

  it('publishManifest posts and refreshes', async () => {
    mockPost.mockResolvedValueOnce({ data: fakeManifest, error: undefined });
    mockGet.mockResolvedValueOnce({ data: [fakeManifest], error: undefined });

    const body = { version: '1.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com/agent', sha256: 'abc123' };
    await useUpdateStore.getState().publishManifest(body);

    expect(mockPost).toHaveBeenCalledWith('/api/v1/updates/manifests', { body });
    expect(useUpdateStore.getState().manifests).toEqual([fakeManifest]);
  });

  it('publishManifest handles error', async () => {
    mockPost.mockResolvedValueOnce({ data: undefined, error: { error: 'bad request' } });

    await useUpdateStore.getState().publishManifest({ version: '1.0.0', os: 'linux', arch: 'amd64', url: '', sha256: '' });

    expect(useUpdateStore.getState().error).toBe('bad request');
  });

  it('pushUpdate returns pushed count', async () => {
    mockPost.mockResolvedValueOnce({ data: { pushed_count: 3 }, error: undefined });

    const count = await useUpdateStore.getState().pushUpdate({ version: '1.0.0', os: 'linux', arch: 'amd64' });

    expect(count).toBe(3);
    expect(mockPost).toHaveBeenCalledWith('/api/v1/updates/push', {
      body: { version: '1.0.0', os: 'linux', arch: 'amd64' },
    });
  });

  it('pushUpdate handles error', async () => {
    mockPost.mockResolvedValueOnce({ data: undefined, error: { error: 'not found' } });

    const count = await useUpdateStore.getState().pushUpdate({ version: '1.0.0', os: 'linux', arch: 'amd64' });

    expect(count).toBeUndefined();
    expect(useUpdateStore.getState().error).toBe('not found');
  });

  it('fetchEnrollmentTokens populates tokens', async () => {
    mockGet.mockResolvedValueOnce({ data: [fakeToken], error: undefined });

    await useUpdateStore.getState().fetchEnrollmentTokens();

    expect(useUpdateStore.getState().enrollmentTokens).toEqual([fakeToken]);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/enrollment-tokens');
  });

  it('fetchEnrollmentTokens handles error', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    await useUpdateStore.getState().fetchEnrollmentTokens();

    expect(useUpdateStore.getState().error).toBe('forbidden');
  });

  it('createEnrollmentToken posts and refreshes', async () => {
    mockPost.mockResolvedValueOnce({ data: fakeToken, error: undefined });
    mockGet.mockResolvedValueOnce({ data: [fakeToken], error: undefined });

    await useUpdateStore.getState().createEnrollmentToken({ label: 'test', max_uses: 5, expires_in_hours: 24 });

    expect(mockPost).toHaveBeenCalledWith('/api/v1/enrollment-tokens', {
      body: { label: 'test', max_uses: 5, expires_in_hours: 24 },
    });
    expect(useUpdateStore.getState().enrollmentTokens).toEqual([fakeToken]);
  });

  it('createEnrollmentToken handles error', async () => {
    mockPost.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    await useUpdateStore.getState().createEnrollmentToken({ label: '', max_uses: 0, expires_in_hours: 24 });

    expect(useUpdateStore.getState().error).toBe('forbidden');
  });

  it('deleteEnrollmentToken deletes and refreshes', async () => {
    mockDelete.mockResolvedValueOnce({ error: undefined });
    mockGet.mockResolvedValueOnce({ data: [], error: undefined });

    await useUpdateStore.getState().deleteEnrollmentToken('t1');

    expect(mockDelete).toHaveBeenCalledWith('/api/v1/enrollment-tokens/{id}', {
      params: { path: { id: 't1' } },
    });
    expect(useUpdateStore.getState().enrollmentTokens).toEqual([]);
  });

  it('deleteEnrollmentToken handles error', async () => {
    mockDelete.mockResolvedValueOnce({ error: { error: 'not found' } });

    await useUpdateStore.getState().deleteEnrollmentToken('t1');

    expect(useUpdateStore.getState().error).toBe('not found');
  });

  it('fetchCACert populates caPem', async () => {
    mockGet.mockResolvedValueOnce({ data: { pem: '-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----' }, error: undefined });

    await useUpdateStore.getState().fetchCACert();

    expect(useUpdateStore.getState().caPem).toContain('BEGIN CERTIFICATE');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/server/ca');
  });

  it('fetchCACert handles error', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'unauthorized' } });

    await useUpdateStore.getState().fetchCACert();

    expect(useUpdateStore.getState().error).toBe('unauthorized');
    expect(useUpdateStore.getState().caPem).toBeNull();
  });
});
