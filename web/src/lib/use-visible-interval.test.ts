import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useVisibleInterval } from './use-visible-interval';

/** Drives document.visibilityState and fires the matching visibilitychange event. */
function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('useVisibleInterval', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setVisibility('visible');
  });

  afterEach(() => {
    vi.useRealTimers();
    setVisibility('visible');
  });

  it('runs the callback on the interval while the tab is visible', () => {
    const cb = vi.fn();
    renderHook(() => { useVisibleInterval(cb, 1000); });

    expect(cb).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(3000); });
    expect(cb).toHaveBeenCalledTimes(3);
  });

  it('issues nothing while the tab is hidden', () => {
    const cb = vi.fn();
    renderHook(() => { useVisibleInterval(cb, 1000); });

    act(() => { setVisibility('hidden'); });
    act(() => { vi.advanceTimersByTime(60_000); });
    expect(cb).not.toHaveBeenCalled();
  });

  it('fires once immediately on re-show, then resumes the interval', () => {
    const cb = vi.fn();
    renderHook(() => { useVisibleInterval(cb, 1000); });

    act(() => { setVisibility('hidden'); });
    act(() => { vi.advanceTimersByTime(60_000); });
    expect(cb).not.toHaveBeenCalled();

    act(() => { setVisibility('visible'); });
    expect(cb).toHaveBeenCalledTimes(1);

    act(() => { vi.advanceTimersByTime(1000); });
    expect(cb).toHaveBeenCalledTimes(2);
  });

  it('does not fire a catch-up on the first visible mount', () => {
    const cb = vi.fn();
    renderHook(() => { useVisibleInterval(cb, 1000); });
    // Mount-time fetches are the caller's job; the hook governs the repeat only.
    expect(cb).not.toHaveBeenCalled();
  });

  it('stops on unmount', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => { useVisibleInterval(cb, 1000); });

    act(() => { vi.advanceTimersByTime(1000); });
    expect(cb).toHaveBeenCalledTimes(1);

    unmount();
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it('does not restart the interval when an inline callback changes identity', () => {
    const inner = vi.fn();
    const { rerender } = renderHook(() => {
      useVisibleInterval(() => { inner(); }, 1000);
    });

    act(() => { vi.advanceTimersByTime(900); });
    rerender();
    rerender();
    // A restarting interval would have reset the 900 ms already elapsed.
    act(() => { vi.advanceTimersByTime(100); });
    expect(inner).toHaveBeenCalledTimes(1);
  });

  it('always calls the latest callback', () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = renderHook(({ cb }: { cb: () => void }) => {
      useVisibleInterval(cb, 1000);
    }, { initialProps: { cb: first } });

    rerender({ cb: second });
    act(() => { vi.advanceTimersByTime(1000); });

    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it('never schedules when the tab starts hidden', () => {
    setVisibility('hidden');
    const cb = vi.fn();
    renderHook(() => { useVisibleInterval(cb, 1000); });

    act(() => { vi.advanceTimersByTime(10_000); });
    expect(cb).not.toHaveBeenCalled();
  });
});
