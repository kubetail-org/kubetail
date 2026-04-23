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

import { useCallback, useEffect } from 'react';

import { usePreferences } from '@/lib/preferences';

export const enum ResolvedTheme {
  Light = 'Light',
  Dark = 'Dark',
}

export const enum Theme {
  System = 'System',
  Light = 'Light',
  Dark = 'Dark',
}

/**
 * Media query helper
 */

function getMediaQuery() {
  return window.matchMedia('(prefers-color-scheme: dark)');
}

/**
 * Derive theme values from preferences
 */

function toTheme(theme?: string): Theme {
  switch (theme) {
    case 'dark':
      return Theme.Dark;
    case 'light':
      return Theme.Light;
    default:
      return Theme.System;
  }
}

function toThemeString(pref: Theme): string {
  switch (pref) {
    case Theme.Dark:
      return 'dark';
    case Theme.Light:
      return 'light';
    default:
      return 'system';
  }
}

function resolveTheme(pref: Theme): ResolvedTheme {
  switch (pref) {
    case Theme.Dark:
      return ResolvedTheme.Dark;
    case Theme.Light:
      return ResolvedTheme.Light;
    default:
      return getMediaQuery().matches ? ResolvedTheme.Dark : ResolvedTheme.Light;
  }
}

/**
 * useTheme hook — derives theme from preferences (pure derivation, no side effects)
 */

export function useTheme() {
  const { preferences, updatePreferences } = usePreferences();
  const theme = toTheme(preferences.theme);
  const resolvedTheme = resolveTheme(theme);

  const setTheme = useCallback(
    (value: Theme) => {
      updatePreferences({ theme: toThemeString(value) });
    },
    [updatePreferences],
  );

  return { resolvedTheme, theme, setTheme };
}

/**
 * ThemeEffect — always-mounted component that keeps the DOM in sync with the
 * current theme (dark class on <html>, OS preference listener).
 */

export function ThemeEffect() {
  const { preferences, updatePreferences } = usePreferences();
  const theme = toTheme(preferences.theme);
  const resolvedTheme = resolveTheme(theme);

  // apply dark class to <html>
  useEffect(() => {
    if (resolvedTheme === ResolvedTheme.Dark) document.documentElement.classList.add('dark');
    else document.documentElement.classList.remove('dark');
  }, [resolvedTheme]);

  // listen for OS/browser preference changes when in system mode
  useEffect(() => {
    const mediaQuery = getMediaQuery();
    const fn = (_ev: MediaQueryListEvent) => {
      if (toTheme(preferences.theme) === Theme.System) {
        updatePreferences({ theme: 'system' });
      }
    };
    mediaQuery.addEventListener('change', fn);
    return () => mediaQuery.removeEventListener('change', fn);
  }, [preferences.theme, updatePreferences]);

  return null;
}
