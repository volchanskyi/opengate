import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAuthStore } from './auth-store';
import { csrfToken, setCsrfToken, SIGNED_IN_KEY } from '../lib/console-session';

const mockPost = vi.fn();
const mockGet = vi.fn();
const mockDelete = vi.fn();

vi.mock('../lib/api', () => ({
  api: {
    POST: (...args: unknown[]) => mockPost(...args),
    GET: (...args: unknown[]) => mockGet(...args),
    DELETE: (...args: unknown[]) => mockDelete(...args),
  },
}));

const TENANT = '00000000-0000-0000-0000-000000000002';

function consoleSession(email: string, displayName = 'A') {
  return {
    user: { id: '1', email, display_name: displayName, is_admin: false, created_at: '', updated_at: '' },
    tenant_id: TENANT,
    csrf_token: 'csrf-from-sign-in',
    idle_timeout_seconds: 1800,
    expires_at: '2026-10-16T17:00:00Z',
  };
}

describe('auth store', () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    setCsrfToken(null);
    vi.clearAllMocks();
    useAuthStore.setState({
      tenantId: null,
      user: null,
      idleTimeoutSeconds: null,
      isLoading: false,
      hydrated: false,
      error: null,
    });
  });

  it('signs in with the session endpoint and keeps no pass in page storage', async () => {
    mockPost.mockResolvedValueOnce({ data: consoleSession('a@b.com'), error: undefined });

    await useAuthStore.getState().login('a@b.com', 'pass');

    expect(mockPost).toHaveBeenCalledWith('/api/v1/auth/session', {
      body: { email: 'a@b.com', password: 'pass' },
    });
    expect(useAuthStore.getState().user?.email).toBe('a@b.com');
    expect(useAuthStore.getState().tenantId).toBe(TENANT);
    expect(useAuthStore.getState().idleTimeoutSeconds).toBe(1800);
    expect(csrfToken()).toBe('csrf-from-sign-in');
    expect(Object.keys(localStorage)).toEqual([SIGNED_IN_KEY]);
    expect(localStorage.getItem(SIGNED_IN_KEY)).toBe(`1/${TENANT}`);
    expect(sessionStorage).toHaveLength(0);
  });

  it('a refused sign-in says why and signs nobody in', async () => {
    mockPost.mockResolvedValueOnce({ data: undefined, error: { error: 'invalid credentials' } });

    await useAuthStore.getState().login('a@b.com', 'wrong');

    expect(useAuthStore.getState().error).toBe('invalid credentials');
    expect(useAuthStore.getState().user).toBeNull();
    expect(csrfToken()).toBeNull();
  });

  it('registers with the session endpoint and is signed in', async () => {
    mockPost.mockResolvedValueOnce({ data: consoleSession('b@c.com', 'B'), error: undefined });

    await useAuthStore.getState().register('b@c.com', 'pass', 'B');

    expect(mockPost).toHaveBeenCalledWith('/api/v1/auth/session/register', {
      body: { email: 'b@c.com', password: 'pass', display_name: 'B' },
    });
    expect(useAuthStore.getState().user?.display_name).toBe('B');
    expect(csrfToken()).toBe('csrf-from-sign-in');
  });

  it('signing out ends the session on the server and forgets it here', async () => {
    mockPost.mockResolvedValueOnce({ data: consoleSession('a@b.com'), error: undefined });
    await useAuthStore.getState().login('a@b.com', 'pass');
    mockDelete.mockResolvedValueOnce({ data: undefined, error: undefined, response: { status: 204 } });

    await useAuthStore.getState().logout();

    expect(mockDelete).toHaveBeenCalledWith('/api/v1/auth/session');
    expect(useAuthStore.getState().user).toBeNull();
    expect(useAuthStore.getState().tenantId).toBeNull();
    expect(csrfToken()).toBeNull();
    expect(localStorage.getItem(SIGNED_IN_KEY)).toBeNull();
  });

  it('a page that reloads asks the server for its session', async () => {
    mockGet.mockResolvedValueOnce({ data: consoleSession('a@b.com'), error: undefined, response: { status: 200 } });

    await useAuthStore.getState().hydrate();

    expect(mockGet).toHaveBeenCalledWith('/api/v1/auth/session');
    expect(useAuthStore.getState().user?.email).toBe('a@b.com');
    expect(csrfToken()).toBe('csrf-from-sign-in');
    expect(useAuthStore.getState().hydrated).toBe(true);
  });

  it('a page that reloads with no live session is signed out', async () => {
    localStorage.setItem(SIGNED_IN_KEY, `1/${TENANT}`);
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'session expired' }, response: { status: 401 } });

    await useAuthStore.getState().hydrate();

    expect(useAuthStore.getState().user).toBeNull();
    expect(localStorage.getItem(SIGNED_IN_KEY)).toBeNull();
    expect(useAuthStore.getState().hydrated).toBe(true);
  });

  it('a 401 from the server signs the console out', async () => {
    mockPost.mockResolvedValueOnce({ data: consoleSession('a@b.com'), error: undefined });
    await useAuthStore.getState().login('a@b.com', 'pass');
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'session expired' }, response: { status: 401 } });

    await useAuthStore.getState().fetchMe();

    expect(useAuthStore.getState().user).toBeNull();
    expect(csrfToken()).toBeNull();
  });

  it('keeps the session when the server fails for a reason other than 401', async () => {
    mockPost.mockResolvedValueOnce({ data: consoleSession('a@b.com'), error: undefined });
    await useAuthStore.getState().login('a@b.com', 'pass');
    mockGet.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'upstream unavailable' },
      response: { status: 503 },
    });

    await useAuthStore.getState().fetchMe();

    expect(useAuthStore.getState().user?.email).toBe('a@b.com');
    expect(csrfToken()).toBe('csrf-from-sign-in');
  });
});
