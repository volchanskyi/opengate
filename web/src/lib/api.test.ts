import { describe, it, expect, beforeEach, vi } from 'vitest';
import createClient, { type Middleware } from 'openapi-fetch';
import { api, QUERY_SERIALIZER } from './api';

// Records the options and middleware of the first client built, which is the one api.ts builds.
const shipped = vi.hoisted(() => ({
  options: null as Record<string, unknown> | null,
  client: null as unknown,
  middleware: [] as unknown[],
}));

vi.mock('openapi-fetch', async (importOriginal) => {
  const actual = await importOriginal<typeof import('openapi-fetch')>();
  const record = (options: Record<string, unknown>) => {
    const client = actual.default(options) as { use: (...m: unknown[]) => void };
    if (shipped.options === null) {
      shipped.options = options;
      shipped.client = client;
      const register = client.use.bind(client);
      client.use = (...middleware: unknown[]) => {
        shipped.middleware.push(...middleware);
        return register(...middleware);
      };
    }
    return client;
  };
  return { ...actual, default: record };
});

const ORIGIN = 'https://opengate.example';

type OnRequestParams = Parameters<NonNullable<Extract<Middleware, { onRequest: unknown }>['onRequest']>>[0];

// Node's Request rejects the client's relative base, so the registered middleware runs directly.
async function attachCredentialTo(request: Request): Promise<Request> {
  expect(shipped.middleware).toHaveLength(1);
  const entry = (shipped.middleware as Middleware[])[0];
  if (entry === undefined) {
    throw new Error('the shipped client registered no middleware');
  }
  const onRequest = 'onRequest' in entry ? entry.onRequest : undefined;
  if (typeof onRequest !== 'function') {
    throw new Error('the shipped client registered no onRequest middleware');
  }
  const result = await onRequest({
    request,
    schemaPath: '/api/v1/health',
    params: {},
    id: 'test-request',
    options: {},
  } as OnRequestParams);
  return result instanceof Request ? result : request;
}

describe('the shared api client', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('is the client the module built, and carries the shared query serializer', () => {
    expect(api).toBe(shipped.client);
    expect(shipped.options?.querySerializer).toBe(QUERY_SERIALIZER);
  });

  it('attaches the technician credential as a Bearer token', async () => {
    localStorage.setItem('token', 'test-jwt-token');

    const request = await attachCredentialTo(new Request(`${ORIGIN}/api/v1/health`));

    expect(request.headers.get('Authorization')).toBe('Bearer test-jwt-token');
  });

  it('sends no credential when nobody is signed in', async () => {
    const request = await attachCredentialTo(new Request(`${ORIGIN}/api/v1/health`));

    expect(request.headers.get('Authorization')).toBeNull();
  });

  it('leaves a header the caller set alone when nobody is signed in', async () => {
    const request = await attachCredentialTo(
      new Request(`${ORIGIN}/api/v1/health`, { headers: { Accept: 'application/json' } }),
    );

    expect(request.headers.get('Accept')).toBe('application/json');
    expect(request.headers.get('Authorization')).toBeNull();
  });
});

describe('api client — repeated query parameters', () => {
  // Node's Request rejects the shared client's relative base, so this client uses an absolute one.
  async function requestUrl(
    call: (client: ReturnType<typeof createClient>) => Promise<unknown>,
  ): Promise<URL> {
    let captured: URL | undefined;
    const client = createClient({ baseUrl: ORIGIN, querySerializer: QUERY_SERIALIZER });
    client.use({
      async onRequest({ request }) {
        captured = new URL(request.url);
        return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
      },
    });
    await call(client);
    if (!captured) throw new Error('no request was issued');
    return captured;
  }

  it('joins a repeated parameter with commas, the form every array in the spec declares', async () => {
    const url = await requestUrl((client) => client.GET('/api/v1/investigations' as never, {
      params: { query: { status: ['new', 'acknowledged'] } },
    } as never));

    expect(url.searchParams.getAll('status')).toEqual(['new,acknowledged']);
  });

  it('applies the same form to the metric dimensions, so the second one is not dropped', async () => {
    const url = await requestUrl((client) => client.GET('/api/v1/devices/{id}/metrics' as never, {
      params: { path: { id: 'dev-7' }, query: { dims: ['cpu.busy_pct', 'mem.used_pct'] } },
    } as never));

    expect(url.searchParams.getAll('dims')).toEqual(['cpu.busy_pct,mem.used_pct']);
  });

  it('leaves a single-valued parameter alone', async () => {
    const url = await requestUrl((client) => client.GET('/api/v1/investigations' as never, {
      params: { query: { rule_id: 'cpu.sustained' } },
    } as never));

    expect(url.searchParams.get('rule_id')).toBe('cpu.sustained');
  });
});
