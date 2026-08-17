import { describe, it, expect, beforeEach } from 'vitest';
import createClient, { type Middleware } from 'openapi-fetch';
import { QUERY_SERIALIZER } from './api';

describe('api client', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('attaches Authorization header when token in localStorage', async () => {
    localStorage.setItem('token', 'test-jwt-token');

    let capturedHeaders: Headers | undefined;
    const middleware: Middleware = {
      async onRequest({ request }) {
        const token = localStorage.getItem('token');
        if (token) {
          request.headers.set('Authorization', `Bearer ${token}`);
        }
        return request;
      },
    };

    const client = createClient({ baseUrl: 'http://localhost' });
    client.use(middleware);
    client.use({
      async onRequest({ request }) {
        capturedHeaders = request.headers;
        return new Response(JSON.stringify({ status: 'ok' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      },
    });

    await client.GET('/api/v1/health' as never);
    expect(capturedHeaders?.get('Authorization')).toBe('Bearer test-jwt-token');
  });

  it('omits Authorization header when no token', async () => {
    let capturedHeaders: Headers | undefined;
    const middleware: Middleware = {
      async onRequest({ request }) {
        const token = localStorage.getItem('token');
        if (token) {
          request.headers.set('Authorization', `Bearer ${token}`);
        }
        return request;
      },
    };

    const client = createClient({ baseUrl: 'http://localhost' });
    client.use(middleware);
    client.use({
      async onRequest({ request }) {
        capturedHeaders = request.headers;
        return new Response(JSON.stringify({ status: 'ok' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      },
    });

    await client.GET('/api/v1/health' as never);
    expect(capturedHeaders?.get('Authorization')).toBeNull();
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
    const client = createClient({ baseUrl: 'http://localhost', querySerializer: QUERY_SERIALIZER });
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
