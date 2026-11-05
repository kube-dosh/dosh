import "@testing-library/jest-dom/vitest";

// jsdom exposes scrollTo but throws "Not implemented" when components call it.
Object.defineProperty(window, "scrollTo", {
  configurable: true,
  writable: true,
  value: () => undefined,
});

Object.defineProperty(window, "matchMedia", {
  configurable: true,
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  }),
});
