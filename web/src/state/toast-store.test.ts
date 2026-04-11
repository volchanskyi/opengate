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

  it('removes toast by id', () => {
    useToastStore.getState().addToast('To remove', 'error');
    const id = useToastStore.getState().toasts[0]!.id;

    useToastStore.getState().removeToast(id);
    expect(useToastStore.getState().toasts).toHaveLength(0);
  });

  it('limits toasts to 5', () => {
    for (let i = 0; i < 7; i++) {
      useToastStore.getState().addToast(`Toast ${i}`, 'info');
    }
    expect(useToastStore.getState().toasts.length).toBeLessThanOrEqual(5);
  });
});
