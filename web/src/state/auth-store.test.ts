import { describe, it, expect, beforeEach, vi } from 'vitest';
import { orgIdFromToken, useAuthStore } from './auth-store';

const mockPost = vi.fn();
const mockGet = vi.fn();

vi.mock('../lib/api', () => ({
  api: {
    POST: (...args: unknown[]) => mockPost(...args),
    GET: (...args: unknown[]) => mockGet(...args),
  },
}));

function jwtWithOrg(orgId: string): string {
  const encode = (value: object) =>
    btoa(JSON.stringify(value)).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
  return `${encode({ alg: 'none' })}.${encode({ org: orgId })}.sig`;
}

describe('auth store', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useAuthStore.setState({
      token: null,
      orgId: null,
      user: null,
      isLoading: false,
      hydrated: false,
      error: null,
    });
  });

  it('login stores token and fetches user', async () => {
    const token = jwtWithOrg('00000000-0000-0000-0000-000000000002');
    mockPost.mockResolvedValueOnce({
      data: { token },
      error: undefined,
    });
    mockGet.mockResolvedValueOnce({
      data: { id: '1', email: 'a@b.com', display_name: 'A', is_admin: false },
      error: undefined,
      response: { status: 200 },
    });

    await useAuthStore.getState().login('a@b.com', 'pass');

    expect(localStorage.getItem('token')).toBe(token);
    expect(useAuthStore.getState().token).toBe(token);
    expect(useAuthStore.getState().orgId).toBe('00000000-0000-0000-0000-000000000002');
    expect(useAuthStore.getState().user?.email).toBe('a@b.com');
    expect(useAuthStore.getState().error).toBeNull();
  });

  it('login error sets error state', async () => {
    mockPost.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'invalid credentials' },
    });

    await useAuthStore.getState().login('a@b.com', 'wrong');

    expect(useAuthStore.getState().error).toBe('invalid credentials');
    expect(useAuthStore.getState().token).toBeNull();
  });

  it('register stores token and fetches user', async () => {
    mockPost.mockResolvedValueOnce({
      data: { token: 'jwt-456' },
      error: undefined,
    });
    mockGet.mockResolvedValueOnce({
      data: { id: '2', email: 'b@c.com', display_name: 'B', is_admin: false },
      error: undefined,
      response: { status: 200 },
    });

    await useAuthStore.getState().register('b@c.com', 'pass', 'B');

    expect(localStorage.getItem('token')).toBe('jwt-456');
    expect(useAuthStore.getState().user?.display_name).toBe('B');
  });

  it('logout clears state', () => {
    localStorage.setItem('token', 'old-token');
    useAuthStore.setState({
      token: 'old-token',
      orgId: '00000000-0000-0000-0000-000000000002',
      user: {
        id: '1',
        email: 'a@b.com',
        display_name: 'A',
        is_admin: false,
        created_at: '',
        updated_at: '',
      },
    });

    useAuthStore.getState().logout();

    expect(localStorage.getItem('token')).toBeNull();
    expect(useAuthStore.getState().token).toBeNull();
    expect(useAuthStore.getState().orgId).toBeNull();
    expect(useAuthStore.getState().user).toBeNull();
  });

  it('hydrate reads token from localStorage and fetches user', async () => {
    const token = jwtWithOrg('11111111-1111-1111-1111-111111111111');
    localStorage.setItem('token', token);
    mockGet.mockResolvedValueOnce({
      data: { id: '1', email: 'a@b.com', display_name: 'A', is_admin: false },
      error: undefined,
      response: { status: 200 },
    });

    await useAuthStore.getState().hydrate();

    expect(useAuthStore.getState().token).toBe(token);
    expect(useAuthStore.getState().orgId).toBe('11111111-1111-1111-1111-111111111111');
    expect(useAuthStore.getState().user?.email).toBe('a@b.com');
    expect(useAuthStore.getState().hydrated).toBe(true);
  });

  it('hydrate does nothing when no token', async () => {
    await useAuthStore.getState().hydrate();

    expect(useAuthStore.getState().token).toBeNull();
    expect(mockGet).not.toHaveBeenCalled();
    expect(useAuthStore.getState().hydrated).toBe(true);
  });

  it('hydrate auto-logouts on 401', async () => {
    localStorage.setItem('token', 'expired-token');
    mockGet.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'invalid token' },
      response: { status: 401 },
    });

    await useAuthStore.getState().hydrate();

    expect(useAuthStore.getState().token).toBeNull();
    expect(localStorage.getItem('token')).toBeNull();
  });

  it('orgIdFromToken fails closed on malformed tokens', () => {
    expect(orgIdFromToken('not-a-jwt')).toBeNull();
    expect(orgIdFromToken('a.b.c')).toBeNull();
  });
});
