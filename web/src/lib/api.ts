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

/** Array query parameters travel comma-joined (`?status=new,acknowledged`), as the spec says. */
export const QUERY_SERIALIZER = { array: { style: 'form', explode: false } } as const;

export const api = createClient<paths>({ baseUrl: '', querySerializer: QUERY_SERIALIZER });
api.use(authMiddleware);
