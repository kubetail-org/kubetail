import '@testing-library/jest-dom/vitest';

// jsdom doesn't implement window.scrollTo (used by router/UI components during navigation)
window.scrollTo = () => {};

// jsdom doesn't implement matchMedia — stub it for components that check media queries
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});
