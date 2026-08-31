import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  reportClientError,
  resetReportErrorState,
  installGlobalErrorReporting,
  sanitizeReportedUrl,
} from './report-error';

// Mirror of the module-private rate-limit window so the pruning test can drive
// the fake clock to the exact age at which a timestamp falls out of the window.
const RATE_WINDOW_MS = 60_000;

describe('reportClientError', () => {
  let beacon: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    resetReportErrorState();
    beacon = vi.fn(() => true);
    vi.stubGlobal('navigator', { sendBeacon: beacon, userAgent: 'test-ua' });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
    vi.useRealTimers();
  });

  it('does not beacon outside production', () => {
    vi.stubEnv('PROD', false);
    expect(reportClientError({ message: 'boom' })).toBe(false);
    expect(beacon).not.toHaveBeenCalled();
  });

  it('does not beacon when navigator is unavailable', () => {
    vi.stubEnv('PROD', true);
    vi.stubGlobal('navigator', undefined);
    expect(reportClientError({ message: 'boom' })).toBe(false);
  });

  it('does not beacon when sendBeacon is not a function', () => {
    vi.stubEnv('PROD', true);
    vi.stubGlobal('navigator', { sendBeacon: undefined, userAgent: 'test-ua' });
    expect(reportClientError({ message: 'boom' })).toBe(false);
  });

  it('beacons in production with a PII-free payload', async () => {
    vi.stubEnv('PROD', true);
    expect(reportClientError({ message: 'boom', source: 'ErrorBoundary' })).toBe(true);
    expect(beacon).toHaveBeenCalledTimes(1);

    const [endpoint, body] = beacon.mock.calls[0]!;
    expect(endpoint).toBe('/api/v1/client-errors');
    const blob = body as Blob;
    // The beacon must be typed application/json so the server parses it as JSON.
    expect(blob.type).toBe('application/json');
    const text = await blob.text();
    const parsed = JSON.parse(text);
    expect(parsed.message).toBe('boom');
    expect(parsed.source).toBe('ErrorBoundary');
    expect(parsed.user_agent).toBe('test-ua');
    expect(parsed).not.toHaveProperty('token');
    expect(parsed).not.toHaveProperty('email');
    expect(text).not.toContain('Bearer');
  });

  it('prefers an explicit url over the current location, reduced to its path', async () => {
    vi.stubEnv('PROD', true);
    reportClientError({ message: 'boom', url: 'https://app.example/explicit' });
    const parsed = JSON.parse(await (beacon.mock.calls[0]![1] as Blob).text());
    expect(parsed.url).toBe('/explicit');
  });

  it('falls back to the current location when no url is supplied', async () => {
    vi.stubEnv('PROD', true);
    reportClientError({ message: 'boom' });
    const parsed = JSON.parse(await (beacon.mock.calls[0]![1] as Blob).text());
    expect(parsed.url).toBe(globalThis.location.pathname);
  });

  // The session route carries the relay token as a path segment. This payload
  // is written to the server log and shipped to Loki, so an un-redacted URL
  // would put a live bearer credential in the log store — the very thing the
  // server's request-log redaction exists to prevent.
  it('redacts a credential-bearing path segment', async () => {
    vi.stubEnv('PROD', true);
    const token = 'a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90';
    reportClientError({ message: 'boom', url: `https://app.example/sessions/${token}` });
    const text = await (beacon.mock.calls[0]![1] as Blob).text();
    expect(text).not.toContain(token);
    expect(JSON.parse(text).url).toBe('/sessions/a1b2c3d4...');
  });

  it('drops the query string and fragment, which can carry credentials', async () => {
    vi.stubEnv('PROD', true);
    reportClientError({
      message: 'boom',
      url: 'https://app.example/devices?auth=supersecrettoken#access_token=alsosecret',
    });
    const text = await (beacon.mock.calls[0]![1] as Blob).text();
    expect(text).not.toContain('supersecrettoken');
    expect(text).not.toContain('alsosecret');
    expect(JSON.parse(text).url).toBe('/devices');
  });

  it('keeps ordinary paths intact', async () => {
    vi.stubEnv('PROD', true);
    reportClientError({ message: 'boom', url: 'https://app.example/devices/abc' });
    const parsed = JSON.parse(await (beacon.mock.calls[0]![1] as Blob).text());
    expect(parsed.url).toBe('/devices/abc');
  });

  it('omits the url without throwing when location is unavailable', () => {
    vi.stubEnv('PROD', true);
    vi.stubGlobal('location', undefined);
    expect(() => reportClientError({ message: 'boom' })).not.toThrow();
    expect(beacon).toHaveBeenCalledTimes(1);
  });

  it('truncates the stack to 500 chars', async () => {
    vi.stubEnv('PROD', true);
    const longStack = 'x'.repeat(5000);
    reportClientError({ message: 'boom', stack: longStack });
    const text = await (beacon.mock.calls[0]![1] as Blob).text();
    expect(JSON.parse(text).stack.length).toBe(500);
  });

  it('enforces a client-side rate limit of 10 per minute', () => {
    vi.stubEnv('PROD', true);
    for (let i = 0; i < 10; i++) {
      expect(reportClientError({ message: `e${i}` })).toBe(true);
    }
    expect(reportClientError({ message: 'overflow' })).toBe(false);
    expect(beacon).toHaveBeenCalledTimes(10);
  });

  it('prunes timestamps once they age out of the rate window', () => {
    vi.stubEnv('PROD', true);
    vi.useFakeTimers();
    vi.setSystemTime(0);
    for (let i = 0; i < 10; i++) {
      expect(reportClientError({ message: `e${i}` })).toBe(true);
    }
    // An 11th report inside the window is rejected.
    expect(reportClientError({ message: 'overflow' })).toBe(false);
    // At exactly RATE_WINDOW_MS the earlier entries are age === window, which the
    // strict `<` filter drops, so a fresh report is admitted again.
    vi.setSystemTime(RATE_WINDOW_MS);
    expect(reportClientError({ message: 'after-window' })).toBe(true);
  });

  it('ignores empty messages', () => {
    vi.stubEnv('PROD', true);
    expect(reportClientError({ message: '' })).toBe(false);
    expect(beacon).not.toHaveBeenCalled();
  });
});

describe('installGlobalErrorReporting', () => {
  let beacon: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    resetReportErrorState();
    beacon = vi.fn(() => true);
    vi.stubGlobal('navigator', { sendBeacon: beacon, userAgent: 'test-ua' });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it('beacons unhandled promise rejections tagged with their source', async () => {
    vi.stubEnv('PROD', true);
    installGlobalErrorReporting();

    const event = new Event('unhandledrejection');
    Object.defineProperty(event, 'reason', { value: new Error('async boom') });
    window.dispatchEvent(event);

    expect(beacon).toHaveBeenCalledTimes(1);
    const parsed = JSON.parse(await (beacon.mock.calls[0]![1] as Blob).text());
    expect(parsed.message).toBe('async boom');
    expect(parsed.source).toBe('unhandledrejection');
    expect(parsed.stack).toBeDefined();
  });
});

// The path reduction is what keeps a bearer credential out of the log store, so
// its edges are tested directly rather than through a beacon.
describe('sanitizeReportedUrl', () => {
  it('reports nothing for a url that was never supplied', () => {
    expect(sanitizeReportedUrl(undefined)).toBeUndefined();
    expect(sanitizeReportedUrl('')).toBeUndefined();
  });

  // A short segment is redacted whole. Showing the first eight characters of an
  // eight-character credential would print the credential.
  it('replaces a credential no longer than the prefix it would show', () => {
    expect(sanitizeReportedUrl('https://app.example/sessions/abcdefgh')).toBe('/sessions/***');
  });

  it('shows only the leading characters of a longer credential', () => {
    expect(sanitizeReportedUrl('https://app.example/sessions/abcdefghi')).toBe('/sessions/abcdefgh...');
  });

  // The prefix on its own carries no credential to redact, and a deeper path is
  // a route rather than a token — redacting either would lose the route without
  // protecting anything.
  it('leaves a credential prefix that carries no credential alone', () => {
    expect(sanitizeReportedUrl('https://app.example/sessions/')).toBe('/sessions/');
    expect(sanitizeReportedUrl('https://app.example/sessions/abcdefghi/frames')).toBe('/sessions/abcdefghi/frames');
  });

  // Every route whose next segment authenticates its holder is covered, not
  // just the first one in the list.
  it('covers every credential-bearing route', () => {
    expect(sanitizeReportedUrl('https://app.example/ws/relay/abcdefghijkl')).toBe('/ws/relay/abcdefgh...');
    expect(sanitizeReportedUrl('https://app.example/api/v1/enroll/abcdefghijkl')).toBe('/api/v1/enroll/abcdefgh...');
  });

  it('keeps an ordinary route as it is', () => {
    expect(sanitizeReportedUrl('https://app.example/devices/abc')).toBe('/devices/abc');
  });

  // A relative url is still reduced to a path even with no page origin to
  // resolve it against, so a caller cannot widen what gets reported.
  it('reduces a relative url with no origin to resolve against', () => {
    vi.stubGlobal('location', undefined);
    try {
      expect(sanitizeReportedUrl('/devices/abc?auth=secret')).toBe('/devices/abc');
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
