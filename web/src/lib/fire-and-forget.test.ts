import { describe, it, expect, vi } from 'vitest';
import { fireAndForget } from './fire-and-forget';

describe('fireAndForget', () => {
  it('logs rejected promise to console.error', async () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const err = new Error('boom');

    fireAndForget(Promise.reject(err));

    // Give microtask a tick to run .catch handler
    await new Promise((r) => { setTimeout(r, 0); });

    expect(consoleSpy).toHaveBeenCalledWith('Unhandled async error:', err);
    consoleSpy.mockRestore();
  });

  it('does nothing for resolved promise', async () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    fireAndForget(Promise.resolve('ok'));

    await new Promise((r) => { setTimeout(r, 0); });

    expect(consoleSpy).not.toHaveBeenCalled();
    consoleSpy.mockRestore();
  });

  it('does nothing for void/undefined input', () => {
    // Should not throw
    fireAndForget(undefined);
    fireAndForget(undefined as void);
  });
});
