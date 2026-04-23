// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import { act, render, screen } from '@testing-library/react';

import { PreferencesProvider } from '@/lib/preferences';

import { Theme, ThemeEffect, useTheme } from './theme';

type MatchMediaListener = (event: MediaQueryListEvent) => void;

function createMatchMediaController(initialMatches = false) {
  let matches = initialMatches;
  const listeners = new Set<MatchMediaListener>();

  const mediaQueryList = {
    get matches() {
      return matches;
    },
    media: '(prefers-color-scheme: dark)',
    onchange: null,
    addEventListener: vi.fn((_event: 'change', listener: MatchMediaListener) => {
      listeners.add(listener);
    }),
    removeEventListener: vi.fn((_event: 'change', listener: MatchMediaListener) => {
      listeners.delete(listener);
    }),
    dispatchEvent: vi.fn(),
  } as unknown as MediaQueryList;

  return {
    matchMedia: vi.fn(() => mediaQueryList),
    setMatches(value: boolean) {
      matches = value;
      const event = { matches: value } as MediaQueryListEvent;
      listeners.forEach((listener) => listener(event));
    },
  };
}

function ThemeConsumer() {
  const { resolvedTheme, theme, setTheme } = useTheme();

  return (
    <div>
      <span data-testid="resolved-theme">{resolvedTheme}</span>
      <span data-testid="theme">{theme}</span>
      <button type="button" onClick={() => setTheme(Theme.System)}>
        system
      </button>
      <button type="button" onClick={() => setTheme(Theme.Light)}>
        light
      </button>
      <button type="button" onClick={() => setTheme(Theme.Dark)}>
        dark
      </button>
    </div>
  );
}

function renderWithProvider() {
  return render(
    <PreferencesProvider>
      <ThemeEffect />
      <ThemeConsumer />
    </PreferencesProvider>,
  );
}

describe('useTheme', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    localStorage.clear();
    document.documentElement.className = '';
  });

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      writable: true,
      value: originalMatchMedia,
    });
  });

  it('resolves to system theme when no preference is set', () => {
    const matchMediaController = createMatchMediaController(true);
    vi.stubGlobal('matchMedia', matchMediaController.matchMedia);

    renderWithProvider();

    expect(screen.getByTestId('theme')).toHaveTextContent(Theme.System);
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('Dark');
    expect(document.documentElement).toHaveClass('dark');
  });

  it('updates resolved theme when system preference changes in system mode', () => {
    const matchMediaController = createMatchMediaController(false);
    vi.stubGlobal('matchMedia', matchMediaController.matchMedia);

    renderWithProvider();

    act(() => {
      matchMediaController.setMatches(true);
    });

    expect(screen.getByTestId('theme')).toHaveTextContent(Theme.System);
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('Dark');
    expect(document.documentElement).toHaveClass('dark');
  });

  it('ignores system preference changes when an explicit theme is selected', () => {
    const matchMediaController = createMatchMediaController(false);
    vi.stubGlobal('matchMedia', matchMediaController.matchMedia);

    renderWithProvider();

    act(() => {
      screen.getByRole('button', { name: 'dark' }).click();
    });

    act(() => {
      matchMediaController.setMatches(false);
    });

    expect(screen.getByTestId('theme')).toHaveTextContent(Theme.Dark);
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('Dark');
    expect(document.documentElement).toHaveClass('dark');
  });

  it('applies dark class to document element for dark theme', () => {
    const matchMediaController = createMatchMediaController(true);
    vi.stubGlobal('matchMedia', matchMediaController.matchMedia);

    renderWithProvider();

    expect(document.documentElement).toHaveClass('dark');
  });

  it('removes dark class from document element for light theme', () => {
    const matchMediaController = createMatchMediaController(false);
    vi.stubGlobal('matchMedia', matchMediaController.matchMedia);

    renderWithProvider();

    expect(document.documentElement).not.toHaveClass('dark');
  });
});
