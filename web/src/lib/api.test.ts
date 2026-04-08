import { describe, it, expect, beforeEach } from 'vitest';
import createClient, { type Middleware } from 'openapi-fetch';

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
