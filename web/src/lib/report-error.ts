// Reports send only message, source, stack, url and user agent; the URL is reduced to a path
// with the query and fragment dropped and credential-bearing segments cut to a short prefix.

const ENDPOINT = '/api/v1/client-errors';
const MAX_MESSAGE = 1000;
const MAX_STACK = 500;
const MAX_FIELD = 300;
const MAX_REPORTS_PER_WINDOW = 10;
const RATE_WINDOW_MS = 60_000;

/** Caller-supplied error context; every field must be free of personal data. */
export interface ClientErrorInput {
  message: string;
  source?: string;
  stack?: string;
  url?: string;
}

let timestamps: number[] = [];

/** Clears the rate-limit window for tests. */
export function resetReportErrorState(): void {
  timestamps = [];
}

function allowReport(now: number): boolean {
  timestamps = timestamps.filter((t) => now - t < RATE_WINDOW_MS);
  if (timestamps.length >= MAX_REPORTS_PER_WINDOW) {
    return false;
  }
  timestamps.push(now);
  return true;
}

function clamp(value: string | undefined, max: number): string | undefined {
  if (value === undefined) {
    return undefined;
  }
  return value.length > max ? value.slice(0, max) : value;
}

// Route prefixes whose next path segment is a bearer credential, as in the server's log redaction.
const CREDENTIAL_PATH_PREFIXES = ['/sessions/', '/ws/relay/', '/api/v1/enroll/'];

// Matches the server's RedactToken: the first 8 characters, or *** when shorter.
function redactToken(token: string): string {
  return token.length <= 8 ? '***' : `${token.slice(0, 8)}...`;
}

/** Reduces a URL to a log-safe path: no query or fragment, credential-bearing segments redacted. */
export function sanitizeReportedUrl(raw: string | undefined): string | undefined {
  if (!raw) {
    return undefined;
  }
  let path: string;
  try {
    path = new URL(raw, globalThis.location?.origin ?? 'http://localhost').pathname;
  } catch {
    path = raw.split(/[?#]/)[0] ?? raw;
  }

  for (const prefix of CREDENTIAL_PATH_PREFIXES) {
    if (!path.startsWith(prefix)) {
      continue;
    }
    const segment = path.slice(prefix.length);
    if (segment === '' || segment.includes('/')) {
      return path;
    }
    return prefix + redactToken(segment);
  }
  return path;
}

/** Queues a beacon and returns true; does nothing outside production or over the rate limit. */
export function reportClientError(input: ClientErrorInput): boolean {
  if (!import.meta.env.PROD) {
    return false;
  }
  if (typeof navigator === 'undefined' || typeof navigator.sendBeacon !== 'function') {
    return false;
  }
  if (!input.message || !allowReport(Date.now())) {
    return false;
  }

  const payload: Record<string, string> = {
    message: clamp(input.message, MAX_MESSAGE)!,
  };
  const source = clamp(input.source, MAX_FIELD);
  if (source) {
    payload.source = source;
  }
  const stack = clamp(input.stack, MAX_STACK);
  if (stack) {
    payload.stack = stack;
  }
  const url = clamp(sanitizeReportedUrl(input.url ?? globalThis.location?.href), MAX_FIELD);
  if (url) {
    payload.url = url;
  }
  const userAgent = clamp(navigator.userAgent, MAX_FIELD);
  if (userAgent) {
    payload.user_agent = userAgent;
  }

  const blob = new Blob([JSON.stringify(payload)], { type: 'application/json' });
  return navigator.sendBeacon(ENDPOINT, blob);
}

/** Reports otherwise-unobserved promise rejections through a global handler. */
export function installGlobalErrorReporting(): void {
  window.addEventListener('unhandledrejection', (event: PromiseRejectionEvent) => {
    const reason = event.reason;
    const message = reason instanceof Error ? reason.message : String(reason);
    const stack = reason instanceof Error ? reason.stack : undefined;
    reportClientError({ message, source: 'unhandledrejection', stack });
  });
}
