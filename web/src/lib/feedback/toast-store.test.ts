import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useToastStore } from './toast-store';

describe('toast-store', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useToastStore.setState({ toasts: [] });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('adds a toast', () => {
    useToastStore.getState().addToast('Hello', 'success');
    const toasts = useToastStore.getState().toasts;
    expect(toasts).toHaveLength(1);
    expect(toasts[0]!.message).toBe('Hello');
    expect(toasts[0]!.type).toBe('success');
  });

  it('auto-removes toast after duration', () => {
    useToastStore.getState().addToast('Ephemeral', 'info', 3000);
    expect(useToastStore.getState().toasts).toHaveLength(1);

    vi.advanceTimersByTime(3000);
    expect(useToastStore.getState().toasts).toHaveLength(0);
  });

  it('removes only the named toast by id, leaving siblings intact', () => {
    useToastStore.getState().addToast('keep-1', 'info');
    useToastStore.getState().addToast('drop-me', 'error');
    useToastStore.getState().addToast('keep-2', 'info');

    const target = useToastStore.getState().toasts.find((t) => t.message === 'drop-me')!;
    useToastStore.getState().removeToast(target.id);

    const remaining = useToastStore.getState().toasts;
    // Pin: target gone, siblings preserved — kills the
    // `filter((t) => t.id !== id)` → `filter(() => false)` mutant (which would
    // drop ALL) and the `() => undefined` mutant (which keeps all).
    expect(remaining.find((t) => t.message === 'drop-me')).toBeUndefined();
    expect(remaining.find((t) => t.message === 'keep-1')).toBeDefined();
    expect(remaining.find((t) => t.message === 'keep-2')).toBeDefined();
  });

  it('auto-remove only drops the specific toast that timed out (not all)', () => {
    useToastStore.getState().addToast('long-lived', 'info', 10_000);
    useToastStore.getState().addToast('short-lived', 'info', 1_000);

    vi.advanceTimersByTime(1_500);
    const remaining = useToastStore.getState().toasts;
    expect(remaining.find((t) => t.message === 'short-lived')).toBeUndefined();
    expect(remaining.find((t) => t.message === 'long-lived')).toBeDefined();
  });

  it('keeps the most recent 5 toasts (drops oldest, NOT newest)', () => {
    for (let i = 0; i < 7; i++) {
      useToastStore.getState().addToast(`Toast ${i}`, 'info');
    }
    const toasts = useToastStore.getState().toasts;
    // Pin: ring buffer holds last 5 → cumulative slice(-4) + new = 5.
    // Kills UnaryOperator `slice(-4)` → `slice(+4)` mutant (which would
    // produce a wrongly-ordered or empty list).
    expect(toasts).toHaveLength(5);
    expect(toasts.map((t) => t.message)).toEqual([
      'Toast 2',
      'Toast 3',
      'Toast 4',
      'Toast 5',
      'Toast 6',
    ]);
  });
});
