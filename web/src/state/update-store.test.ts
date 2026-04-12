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

  it('cleanupInactiveTokens deletes expired and exhausted tokens', async () => {
    const expiredToken = { ...fakeToken, id: 'expired1', expires_at: '2020-01-01T00:00:00Z', use_count: 0, max_uses: 5 };
    const exhaustedToken = { ...fakeToken, id: 'exhausted1', expires_at: '2099-01-01T00:00:00Z', use_count: 5, max_uses: 5 };
    const activeToken = { ...fakeToken, id: 'active1', expires_at: '2099-01-01T00:00:00Z', use_count: 1, max_uses: 5 };

    useUpdateStore.setState({ enrollmentTokens: [expiredToken, exhaustedToken, activeToken] });
    mockDelete.mockResolvedValue({ error: undefined });
    mockGet.mockResolvedValueOnce({ data: [activeToken], error: undefined });

    const count = await useUpdateStore.getState().cleanupInactiveTokens();

    expect(count).toBe(2);
    expect(mockDelete).toHaveBeenCalledTimes(2);
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/enrollment-tokens/{id}', { params: { path: { id: 'expired1' } } });
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/enrollment-tokens/{id}', { params: { path: { id: 'exhausted1' } } });
    expect(useUpdateStore.getState().enrollmentTokens).toEqual([activeToken]);
  });

  it('cleanupInactiveTokens returns 0 when all tokens are active', async () => {
    const activeToken = { ...fakeToken, id: 'active1', expires_at: '2099-01-01T00:00:00Z', use_count: 0, max_uses: 5 };
    useUpdateStore.setState({ enrollmentTokens: [activeToken] });
    mockGet.mockResolvedValueOnce({ data: [activeToken], error: undefined });

    const count = await useUpdateStore.getState().cleanupInactiveTokens();

    expect(count).toBe(0);
    expect(mockDelete).not.toHaveBeenCalled();
  });

  it('cleanupInactiveTokens counts only successful deletions', async () => {
    const expiredToken1 = { ...fakeToken, id: 'e1', expires_at: '2020-01-01T00:00:00Z' };
    const expiredToken2 = { ...fakeToken, id: 'e2', expires_at: '2020-01-01T00:00:00Z' };

    useUpdateStore.setState({ enrollmentTokens: [expiredToken1, expiredToken2] });
    mockDelete
      .mockResolvedValueOnce({ error: undefined })
      .mockResolvedValueOnce({ error: { error: 'server error' } });
    mockGet.mockResolvedValueOnce({ data: [expiredToken2], error: undefined });

    const count = await useUpdateStore.getState().cleanupInactiveTokens();

    expect(count).toBe(1);
  });
});
