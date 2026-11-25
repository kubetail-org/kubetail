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

// Theming controller, two orthogonal axes: (1) color scheme — a
// `ColorSchemePreference` (`system`/`light`/`dark`) resolved to a `ColorScheme`
// that toggles the `.dark` class Tailwind keys on; (2) theme (skin) — the named
// palette within a scheme, not built yet ("theme" is reserved for it;
// `resolveColorScheme` is the seam). The preference's source of truth is the host's
// `host.json` (see `lib/host-file.ts`); the `index.html` inline script hand-mirrors
// `resolveColorScheme` for first paint.
// See docs/adr/2026-08-09-first-paint-theming.md
import type { ReactNode } from 'react';
import { createContext, useContext, useEffect, useMemo, useState } from 'react';

import { readInjectedHostFile, subscribeHostFile, updateHostFile } from '@/lib/host-file';

// The user's choice (includes "system"); resolves to a concrete `ColorScheme`.
export type ColorSchemePreference = 'system' | 'light' | 'dark';
// The resolved color scheme actually painted.
export type ColorScheme = 'light' | 'dark';

const DEFAULT_PREFERENCE: ColorSchemePreference = 'system';

function isColorSchemePreference(value: unknown): value is ColorSchemePreference {
  return value === 'system' || value === 'light' || value === 'dark';
}

// Preference from the injected `host.json` snapshot; defaults to "system" when
// absent, unset, or unrecognized.
export function readInjectedPreference(): ColorSchemePreference {
  const value = readInjectedHostFile().colorSchemePreference;
  return isColorSchemePreference(value) ? value : DEFAULT_PREFERENCE;
}

function systemPrefersDark(): boolean {
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

// "system" defers to the OS. The seam for the future skin axis.
export function resolveColorScheme(preference: ColorSchemePreference): ColorScheme {
  if (preference === 'system') return systemPrefersDark() ? 'dark' : 'light';
  return preference;
}

// Mirror the resolved scheme onto `<html>` as `.dark`. When the skin axis lands,
// this also sets `data-theme`.
export function applyPreference(preference: ColorSchemePreference): void {
  document.documentElement.classList.toggle('dark', resolveColorScheme(preference) === 'dark');
}

type ThemeContextValue = {
  preference: ColorSchemePreference;
  setPreference: (preference: ColorSchemePreference) => void;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

// Owns the color-scheme preference; the future skin selection lands here too
// (hence the "Theme" umbrella name).
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<ColorSchemePreference>(() => readInjectedPreference());

  // Re-apply on preference change and, while "system", on OS scheme flips; an
  // explicit Light/Dark choice ignores the OS entirely.
  useEffect(() => {
    applyPreference(preference);
    if (preference !== 'system') return undefined;
    const mql = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = () => applyPreference('system');
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, [preference]);

  // Track the host's post-write broadcast so any window's change lands here. An
  // unrecognized value is ignored, not reset — a newer host format can't yank the
  // scheme out from under us.
  useEffect(
    () =>
      subscribeHostFile((file) => {
        const value = file.colorSchemePreference;
        if (isColorSchemePreference(value)) setPreferenceState(value);
      }),
    [],
  );

  const value = useMemo<ThemeContextValue>(
    () => ({
      preference,
      setPreference: (next) => {
        // Optimistic: apply locally now, persist in the background.
        setPreferenceState(next);
        updateHostFile({ colorSchemePreference: next });
      },
    }),
    [preference],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

// Named for the scheme axis, leaving room for a future `useTheme`/skin hook.
export function useColorScheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useColorScheme must be used within a ThemeProvider');
  return ctx;
}
