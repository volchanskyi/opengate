import { describe, it, expect, vi } from 'vitest';
import { apiAction } from './api-action';

// openapi-fetch always returns the raw Response; these tests only need ok + status.
function fakeResponse(status: number): Response {
  return { ok: status >= 200 && status < 300, status } as unknown as Response;
}

describe('apiAction', () => {
  it('with loading=true toggles isLoading and clears error', async () => {
    const set = vi.fn();
    const res = await apiAction(set, async () => ({ data: 42 }));

    expect(res).toEqual({ ok: true, data: 42 });
    expect(set).toHaveBeenCalledTimes(2);
    expect(set).toHaveBeenNthCalledWith(1, { isLoading: true, error: null });
    expect(set).toHaveBeenNthCalledWith(2, { isLoading: false });
  });

  it('with loading=true and error sets isLoading false + error message', async () => {
    const set = vi.fn();
    const res = await apiAction(set, async () => ({ error: { error: 'boom' } }));

    expect(res).toEqual({ ok: false });
    expect(set).toHaveBeenCalledTimes(2);
    expect(set).toHaveBeenNthCalledWith(1, { isLoading: true, error: null });
    expect(set).toHaveBeenNthCalledWith(2, { isLoading: false, error: 'boom' });
  });

  it('with loading=false skips isLoading toggle on success', async () => {
    const set = vi.fn();
    const res = await apiAction(set, async () => ({ data: 'ok' }), false);

    expect(res).toEqual({ ok: true, data: 'ok' });
    // Only the initial { error: null } reset; no second call (no isLoading: false)
    expect(set).toHaveBeenCalledTimes(1);
    expect(set).toHaveBeenNthCalledWith(1, { error: null });
  });

  it('with loading=false skips isLoading toggle on error', async () => {
    const set = vi.fn();
    const res = await apiAction(set, async () => ({ error: { error: 'nope' } }), false);

    expect(res).toEqual({ ok: false });
    expect(set).toHaveBeenCalledTimes(2);
    expect(set).toHaveBeenNthCalledWith(1, { error: null });
    expect(set).toHaveBeenNthCalledWith(2, { error: 'nope' });
  });

  it('default loading parameter is true', async () => {
    // Intentionally omit loading; default should behave like loading=true.
    const set = vi.fn();
    await apiAction(set, async () => ({ data: 1 }));

    // Two calls: { isLoading: true, error: null } then { isLoading: false }
    // Specifically: the first call MUST contain isLoading:true (kills `loading = false` default mutant).
    expect(set.mock.calls[0]?.[0]).toEqual({ isLoading: true, error: null });
    expect(set.mock.calls[1]?.[0]).toEqual({ isLoading: false });
  });

  it('does not invoke fn when set throws synchronously? — no, fn always invoked', async () => {
    const fn = vi.fn(async () => ({ data: 'x' }));
    const set = vi.fn();
    await apiAction(set, fn);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('treats a non-ok response with no JSON error body as failure', async () => {
    const set = vi.fn();
    // openapi-fetch leaves `error` undefined for an empty-bodied 409, so a
    // response-blind apiAction would misread this as success.
    const res = await apiAction(set, async () => ({
      data: undefined,
      error: undefined,
      response: fakeResponse(409),
    }));

    expect(res).toEqual({ ok: false });
    expect(set).toHaveBeenNthCalledWith(2, {
      isLoading: false,
      error: 'Request failed with status 409',
    });
  });

  it('treats a non-ok response with an empty-string error body as failure', async () => {
    const set = vi.fn();
    // openapi-fetch yields error: '' for an empty body with no Content-Length.
    const res = await apiAction(set, async () => ({
      error: '',
      response: fakeResponse(500),
    }));

    expect(res).toEqual({ ok: false });
    expect(set).toHaveBeenNthCalledWith(2, {
      isLoading: false,
      error: 'Request failed with status 500',
    });
  });

  it('uses a non-JSON (plain-text) error body as the message', async () => {
    const set = vi.fn();
    const res = await apiAction(
      set,
      async () => ({ error: 'Bad Gateway', response: fakeResponse(502) }),
      false,
    );

    expect(res).toEqual({ ok: false });
    expect(set).toHaveBeenNthCalledWith(2, { error: 'Bad Gateway' });
  });

  it('prefers the JSON {error} message over the status fallback', async () => {
    const set = vi.fn();
    const res = await apiAction(set, async () => ({
      error: { error: 'conflict' },
      response: fakeResponse(409),
    }));

    expect(res).toEqual({ ok: false });
    expect(set).toHaveBeenNthCalledWith(2, { isLoading: false, error: 'conflict' });
  });

  it('treats an ok response with undefined error as success', async () => {
    const set = vi.fn();
    const res = await apiAction(set, async () => ({
      data: 7,
      response: fakeResponse(200),
    }));

    expect(res).toEqual({ ok: true, data: 7 });
    expect(set).toHaveBeenNthCalledWith(2, { isLoading: false });
  });
});
