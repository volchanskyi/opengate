import '@testing-library/jest-dom/vitest'

// jsdom has no layout or ResizeObserver; a fixed viewport lets virtualized lists render rows.
// A test needing another rect overrides getBoundingClientRect on its element.
const VIEWPORT_WIDTH = 1200
const VIEWPORT_HEIGHT = 800

class ResizeObserverStub implements ResizeObserver {
  private readonly callback: ResizeObserverCallback

  constructor(callback: ResizeObserverCallback) {
    this.callback = callback
  }

  observe(target: Element): void {
    // Fires synchronously with the stubbed rect so layout settles within the render's act().
    const rect = target.getBoundingClientRect()
    this.callback(
      [{ target, contentRect: rect } as unknown as ResizeObserverEntry],
      this,
    )
  }

  unobserve(): void {
    // jsdom has nothing to stop observing.
  }

  disconnect(): void {
    // jsdom has nothing to disconnect.
  }
}

globalThis.ResizeObserver = ResizeObserverStub

// uPlot reads matchMedia at import and components call scrollIntoView; jsdom implements neither.
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  configurable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener(): void { /* The deprecated listener API stays inert. */ },
    removeListener(): void { /* The deprecated listener API stays inert. */ },
    addEventListener(): void { /* jsdom evaluates no media queries, so nothing fires. */ },
    removeEventListener(): void { /* Nothing is registered to remove. */ },
    dispatchEvent: (): boolean => false,
  }),
})

Element.prototype.scrollIntoView = function scrollIntoView(): void {
  // jsdom has no layout engine to scroll.
}

// virtual-core reads offsetWidth/offsetHeight, which jsdom returns as 0, so they are stubbed too.
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
