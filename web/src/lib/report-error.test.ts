import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  reportClientError,
  resetReportErrorState,
  installGlobalErrorReporting,
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

  it('prefers an explicit url over the current location', async () => {
    vi.stubEnv('PROD', true);
    reportClientError({ message: 'boom', url: 'https://app.example/explicit' });
    const parsed = JSON.parse(await (beacon.mock.calls[0]![1] as Blob).text());
    expect(parsed.url).toBe('https://app.example/explicit');
  });

  it('falls back to the current location when no url is supplied', async () => {
    vi.stubEnv('PROD', true);
    reportClientError({ message: 'boom' });
    const parsed = JSON.parse(await (beacon.mock.calls[0]![1] as Blob).text());
    expect(parsed.url).toBe(globalThis.location.href);
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
