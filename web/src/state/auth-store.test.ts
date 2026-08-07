import { describe, it, expect, beforeEach, vi } from 'vitest';
import { tenantIdFromToken, useAuthStore } from './auth-store';

const mockPost = vi.fn();
const mockGet = vi.fn();

vi.mock('../lib/api', () => ({
  api: {
    POST: (...args: unknown[]) => mockPost(...args),
    GET: (...args: unknown[]) => mockGet(...args),
  },
}));

function jwtWithTenant(tenantId: string): string {
  const encode = (value: object) =>
    btoa(JSON.stringify(value)).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
  return `${encode({ alg: 'none' })}.${encode({ tenant: tenantId })}.sig`;
}

describe('auth store', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useAuthStore.setState({
      token: null,
      tenantId: null,
      user: null,
      isLoading: false,
      hydrated: false,
      error: null,
    });
  });

  it('login stores token and fetches user', async () => {
    const token = jwtWithTenant('00000000-0000-0000-0000-000000000002');
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
    expect(useAuthStore.getState().tenantId).toBe('00000000-0000-0000-0000-000000000002');
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
    const token = jwtWithTenant('33333333-3333-3333-3333-333333333333');
    mockPost.mockResolvedValueOnce({
      data: { token },
      error: undefined,
    });
    mockGet.mockResolvedValueOnce({
      data: { id: '2', email: 'b@c.com', display_name: 'B', is_admin: false },
      error: undefined,
      response: { status: 200 },
    });

    await useAuthStore.getState().register('b@c.com', 'pass', 'B');

    expect(localStorage.getItem('token')).toBe(token);
    expect(useAuthStore.getState().token).toBe(token);
    expect(useAuthStore.getState().tenantId).toBe('33333333-3333-3333-3333-333333333333');
    expect(useAuthStore.getState().user?.display_name).toBe('B');
  });

  it('logout clears state', () => {
    localStorage.setItem('token', 'old-token');
    useAuthStore.setState({
      token: 'old-token',
      tenantId: '00000000-0000-0000-0000-000000000002',
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
    expect(useAuthStore.getState().tenantId).toBeNull();
    expect(useAuthStore.getState().user).toBeNull();
  });

  it('hydrate reads token from localStorage and fetches user', async () => {
    const token = jwtWithTenant('11111111-1111-1111-1111-111111111111');
    localStorage.setItem('token', token);
    mockGet.mockResolvedValueOnce({
      data: { id: '1', email: 'a@b.com', display_name: 'A', is_admin: false },
      error: undefined,
      response: { status: 200 },
    });

    await useAuthStore.getState().hydrate();

    expect(useAuthStore.getState().token).toBe(token);
    expect(useAuthStore.getState().tenantId).toBe('11111111-1111-1111-1111-111111111111');
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

  it('keeps the session when fetchMe fails for a reason other than 401', async () => {
    useAuthStore.setState({ token: 'live-token' });
    localStorage.setItem('token', 'live-token');
    mockGet.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'upstream unavailable' },
      response: { status: 503 },
    });

    await useAuthStore.getState().fetchMe();

    expect(useAuthStore.getState().token).toBe('live-token');
    expect(localStorage.getItem('token')).toBe('live-token');
  });

  it('tenantIdFromToken fails closed on malformed tokens', () => {
    expect(tenantIdFromToken('not-a-jwt')).toBeNull();
    expect(tenantIdFromToken('a.b.c')).toBeNull();
  });

  it('tenantIdFromToken decodes a base64url payload needing both substitutions and padding', () => {
    // This tenant encodes to a payload carrying '-' and '_' (base64url's stand-ins
    // for '+' and '/') whose length is not a multiple of four, so decoding it
    // exercises both character substitutions and the '=' padding together.
    const tenant = '_%Y~NBDRz?rZS';
    const token = jwtWithTenant(tenant);
    const payload = token.split('.')[1] ?? '';
    expect(payload).toContain('-');
    expect(payload).toContain('_');
    expect(payload.length % 4).not.toBe(0);

    expect(tenantIdFromToken(token)).toBe(tenant);
  });

  it('tenantIdFromToken treats an absent, empty, or non-string tenant as no tenant', () => {
    const encode = (value: object) =>
      btoa(JSON.stringify(value)).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
    const tokenWithClaims = (claims: object) => `${encode({ alg: 'none' })}.${encode(claims)}.sig`;

    expect(tenantIdFromToken(tokenWithClaims({}))).toBeNull();
    expect(tenantIdFromToken(jwtWithTenant(''))).toBeNull();
    expect(tenantIdFromToken(tokenWithClaims({ tenant: 42 }))).toBeNull();

    // A token still held from before the tenancy rename names its tenant under
    // a key this build does not read, so it reads as no tenant. The server
    // answers 401 for the same token and the store logs out from there.
    expect(tenantIdFromToken(tokenWithClaims({ org: crypto.randomUUID() }))).toBeNull();
  });
});
