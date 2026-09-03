import { describe, it, expect, beforeEach, vi } from 'vitest';
import createClient, { type Middleware } from 'openapi-fetch';
import { api, QUERY_SERIALIZER } from './api';

/**
 * What the module hands to openapi-fetch when it is loaded.
 *
 * The credential is attached by a middleware the module registers on the shared
 * client, so the only thing worth asserting is that *that* middleware, on *that*
 * client, does it — a copy of the same code in this file would pass whatever
 * production did. openapi-fetch is a genuine third-party boundary, so the mock
 * sits there: it delegates to the real implementation and records the options
 * and the middleware the shipped client is built with. Only the first client is
 * recorded, which is the one `api.ts` builds at module load; the serializer
 * cases below build their own and are none of its business.
 */
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

/** The base a browser resolves the module's relative base against. */
const ORIGIN = 'https://opengate.example';

type OnRequestParams = Parameters<NonNullable<Extract<Middleware, { onRequest: unknown }>['onRequest']>>[0];

/**
 * Run the middleware the shipped client actually registered, against a request
 * carrying the absolute URL a browser would have resolved. Node's Request
 * constructor rejects the relative base the module is built with, which is what
 * makes driving the client itself impossible here — the middleware, though, is
 * the whole of the behaviour, and it is reachable as the module registered it.
 */
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
  /**
   * The shared client is built against a relative base, which the browser
   * resolves and node's Request constructor rejects, so the serializer is
   * exercised through a client carrying the very same setting.
   */
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
