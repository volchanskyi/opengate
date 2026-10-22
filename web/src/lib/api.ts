import createClient, { type Middleware } from 'openapi-fetch';
import type { paths } from '../types/api';
import { csrfToken, noteRequest } from './console-session';

/** Reads never change anything, so only the other methods carry the forged-request value. */
const READ_METHODS = new Set(['GET', 'HEAD', 'OPTIONS']);

/**
 * The session cookie rides on every same-origin request by itself; this adds the forged-request
 * value to a change and records the request against the idle clock.
 */
const consoleMiddleware: Middleware = {
  async onRequest({ request }) {
    noteRequest();
    const csrf = csrfToken();
    if (csrf && !READ_METHODS.has(request.method)) {
      request.headers.set('X-CSRF-Token', csrf);
    }
    return request;
  },
};

/** Array query parameters travel comma-joined (`?status=new,acknowledged`), as the spec says. */
export const QUERY_SERIALIZER = { array: { style: 'form', explode: false } } as const;

export const api = createClient<paths>({ baseUrl: '', querySerializer: QUERY_SERIALIZER });
api.use(consoleMiddleware);
