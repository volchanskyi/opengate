import { describe, it, expect, vi } from 'vitest';
import { apiAction } from './api-action';

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
});
