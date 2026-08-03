/**
 * Frontend error reporter — ships browser crashes to the server log (Loki) so
 * production client errors are observable to operators.
 *
 * Constraints (see audit-frontend-security-perf.md):
 *  - PROD only: never beacons from dev/test builds.
 *  - PII-free: only message/source/stack/url/userAgent are sent — never tokens,
 *    credentials, or user identifiers. The URL is reduced to a path with the
 *    query, fragment, and any credential-bearing segment stripped.
 *  - Bounded: stack truncated to 500 chars, message to 1000.
 *  - Rate-limited: at most MAX_REPORTS_PER_WINDOW within RATE_WINDOW_MS.
 *  - Self-hosted: uses navigator.sendBeacon to our own endpoint, no SaaS tracker.
 */

const ENDPOINT = '/api/v1/client-errors';
const MAX_MESSAGE = 1000;
const MAX_STACK = 500;
const MAX_FIELD = 300;
const MAX_REPORTS_PER_WINDOW = 10;
const RATE_WINDOW_MS = 60_000;

/** Caller-supplied error context. Keep this PII-free. */
export interface ClientErrorInput {
  message: string;
  source?: string;
  stack?: string;
  url?: string;
}

let timestamps: number[] = [];

/** Reset rate-limit state. Test-only. */
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

/**
 * Route prefixes whose next path segment is itself a bearer credential:
 * possession of the segment authenticates the holder. Mirrors the server's
 * request-log redaction, because this payload lands in the same log store.
 */
const CREDENTIAL_PATH_PREFIXES = ['/sessions/', '/ws/relay/', '/api/v1/enroll/'];

/** Matches the server's RedactToken: first 8 characters, or *** if shorter. */
function redactToken(token: string): string {
  return token.length <= 8 ? '***' : `${token.slice(0, 8)}...`;
}

/**
 * Reduce a URL to a log-safe path.
 *
 * The query string and fragment are dropped wholesale — they are the usual
 * carriers of tokens — and any credential-bearing path segment is truncated.
 * A relative or unparseable value is treated as a path directly, so a caller
 * can never widen what gets reported by passing something unusual.
 */
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

/**
 * Report a client error. No-op outside production, when sendBeacon is
 * unavailable, or when the client-side rate limit is exceeded. Returns true
 * when a beacon was queued.
 */
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

/**
 * Install a global handler that reports otherwise-unobserved promise
 * rejections. Idempotent per call site; safe to call once at startup.
 */
export function installGlobalErrorReporting(): void {
  window.addEventListener('unhandledrejection', (event: PromiseRejectionEvent) => {
    const reason = event.reason;
    const message = reason instanceof Error ? reason.message : String(reason);
    const stack = reason instanceof Error ? reason.stack : undefined;
    reportClientError({ message, source: 'unhandledrejection', stack });
  });
}
