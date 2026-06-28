import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { reportClientError, resetReportErrorState } from './report-error';

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
  });

  it('does not beacon outside production', () => {
    vi.stubEnv('PROD', false);
    expect(reportClientError({ message: 'boom' })).toBe(false);
    expect(beacon).not.toHaveBeenCalled();
  });

  it('beacons in production with a PII-free payload', async () => {
    vi.stubEnv('PROD', true);
    expect(reportClientError({ message: 'boom', source: 'ErrorBoundary' })).toBe(true);
    expect(beacon).toHaveBeenCalledTimes(1);

    const [endpoint, body] = beacon.mock.calls[0]!;
    expect(endpoint).toBe('/api/v1/client-errors');
    const text = await (body as Blob).text();
    const parsed = JSON.parse(text);
    expect(parsed.message).toBe('boom');
    expect(parsed.source).toBe('ErrorBoundary');
    expect(parsed).not.toHaveProperty('token');
    expect(parsed).not.toHaveProperty('email');
    expect(text).not.toContain('Bearer');
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

  it('ignores empty messages', () => {
    vi.stubEnv('PROD', true);
    expect(reportClientError({ message: '' })).toBe(false);
    expect(beacon).not.toHaveBeenCalled();
  });
});
