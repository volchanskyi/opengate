import '@testing-library/jest-dom/vitest'

// jsdom has no layout engine: every element reports a zero-sized rect and there
// is no ResizeObserver. @tanstack/react-virtual measures its scroll element to
// decide which rows fall in the viewport, so without these stubs it would either
// throw (missing ResizeObserver) or window down to nothing (zero-height
// viewport). Provide a fixed, non-zero viewport so virtualized lists render a
// realistic window in tests. Tests that need a specific rect still override
// getBoundingClientRect on the individual element, which takes precedence.
const VIEWPORT_WIDTH = 1200
const VIEWPORT_HEIGHT = 800

class ResizeObserverStub implements ResizeObserver {
  private readonly callback: ResizeObserverCallback

  constructor(callback: ResizeObserverCallback) {
    this.callback = callback
  }

  observe(target: Element): void {
    // Fire synchronously with the element's (stubbed) rect so consumers that
    // derive layout from the first observation settle within the render's act().
    const rect = target.getBoundingClientRect()
    this.callback(
      [{ target, contentRect: rect } as unknown as ResizeObserverEntry],
      this,
    )
  }

  unobserve(): void {
    // no-op: jsdom has nothing to stop observing
  }

  disconnect(): void {
    // no-op: jsdom has nothing to disconnect
  }
}

globalThis.ResizeObserver = ResizeObserverStub

// @tanstack/virtual-core sizes its scroll viewport from offsetWidth/offsetHeight
// (jsdom returns 0 for both), so stub them alongside getBoundingClientRect.
Object.defineProperty(HTMLElement.prototype, 'offsetWidth', {
  configurable: true,
  get: () => VIEWPORT_WIDTH,
})
Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
  configurable: true,
  get: () => VIEWPORT_HEIGHT,
})

Element.prototype.getBoundingClientRect = function getBoundingClientRect(): DOMRect {
  return {
    width: VIEWPORT_WIDTH,
    height: VIEWPORT_HEIGHT,
    top: 0,
    left: 0,
    right: VIEWPORT_WIDTH,
    bottom: VIEWPORT_HEIGHT,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  }
}
