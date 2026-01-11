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

import { createContext, useContext, useEffect, useMemo, useState } from 'react';

const storageKey = 'kubetail:theme';

export const enum Theme {
  Light = 'Light',
  Dark = 'Dark',
}

export const enum UserPreference {
  System = 'System',
  Light = 'Light',
  Dark = 'Dark',
}

type ContextType = {
  theme: Theme;
  userPreference: UserPreference;
  setUserPreference: React.Dispatch<UserPreference>;
};

const Context = createContext({} as ContextType);

/**
 * Media query helper
 */

function getMediaQuery() {
  return window.matchMedia('(prefers-color-scheme: dark)');
}

/**
 * Non-reactive helper methods
 */

function getUserPreference() {
  if (!(storageKey in localStorage)) return UserPreference.System;
  return localStorage[storageKey] === 'dark' ? UserPreference.Dark : UserPreference.Light;
}

function getSystemTheme(ev?: MediaQueryListEvent) {
  return (ev || getMediaQuery()).matches ? Theme.Dark : Theme.Light;
}

function getTheme() {
  switch (getUserPreference()) {
    case UserPreference.System:
      return getSystemTheme();
    case UserPreference.Dark:
      return Theme.Dark;
    case UserPreference.Light:
      return Theme.Light;
    default:
      throw new Error('not implemented');
  }
}

/**
 * Theme hook
 */

export function useTheme() {
  const { theme, userPreference, setUserPreference } = useContext(Context);

  return {
    theme,
    userPreference,
    setUserPreference,
  };
}

/**
 * Theme provider
 */

export function ThemeProvider({ children }: React.PropsWithChildren) {
  const [userPreference, setUserPreference] = useState(getUserPreference);
  const [theme, setTheme] = useState(getTheme);

  // apply theme to dom
  useEffect(() => {
    if (theme === Theme.Dark) document.documentElement.classList.add('dark');
    else document.documentElement.classList.remove('dark');
  }, [theme]);

  // listen for os/browser preference changes
  useEffect(() => {
    const mediaQuery = getMediaQuery();
    const fn = (ev: MediaQueryListEvent) => {
      if (getUserPreference() === UserPreference.System) setTheme(getSystemTheme(ev));
    };
    mediaQuery.addEventListener('change', fn);

    // cleanup
    return () => mediaQuery.removeEventListener('change', fn);
  }, []);

  const context = useMemo(
    () => ({
      theme,
      userPreference,
      setUserPreference: (value: UserPreference) => {
        // upate localStorage
        switch (value) {
          case UserPreference.System:
            localStorage.removeItem(storageKey);
            break;
          case UserPreference.Dark:
            localStorage.setItem(storageKey, 'dark');
            break;
          case UserPreference.Light:
            localStorage.setItem(storageKey, 'light');
            break;
          default:
            throw new Error('not implemented');
        }

        // update react states
        setUserPreference(getUserPreference());
        setTheme(getTheme());
      },
    }),
    [theme, userPreference, setUserPreference, setTheme],
  );

  return <Context.Provider value={context}>{children}</Context.Provider>;
}
