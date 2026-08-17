import createClient, { type Middleware } from 'openapi-fetch';
import type { paths } from '../types/api';

const authMiddleware: Middleware = {
  async onRequest({ request }) {
    const token = localStorage.getItem('token');
    if (token) {
      request.headers.set('Authorization', `Bearer ${token}`);
    }
    return request;
  },
};

/**
 * Every array-valued query parameter in the spec is declared non-exploded, so a
 * repeated value travels comma-joined (`?status=new,acknowledged`). The server's
 * binder reads such a parameter as one comma-separated string and would take
 * only the first value of an exploded list — every value after the first would
 * be dropped without an error anywhere.
 */
export const QUERY_SERIALIZER = { array: { style: 'form', explode: false } } as const;

export const api = createClient<paths>({ baseUrl: '', querySerializer: QUERY_SERIALIZER });
api.use(authMiddleware);
