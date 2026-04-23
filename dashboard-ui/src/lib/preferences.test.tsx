// Copyright 2024-2025 Andres Morey
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

import { act, render, screen, waitFor } from '@testing-library/react';

import { PreferencesProvider, createPreferencesBackend, preferencesBackend, usePreferences } from './preferences';

const STORAGE_KEY = 'kubetail:preferences';

describe('LocalStoragePreferencesBackend', () => {
  let backend: ReturnType<typeof createPreferencesBackend>;

  beforeEach(() => {
    localStorage.clear();
    backend = createPreferencesBackend('cluster');
  });

  it('load returns defaults when localStorage is empty', async () => {
    const prefs = await backend.load();
    expect(prefs).toEqual({ version: 1, theme: 'system' });
  });

  it('load reads existing preferences from localStorage', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ version: 1, theme: 'dark' }));
    const prefs = await backend.load();
    expect(prefs).toEqual({ version: 1, theme: 'dark' });
  });

  it('load returns defaults for malformed JSON', async () => {
    localStorage.setItem(STORAGE_KEY, '{bad json');
    const prefs = await backend.load();
    expect(prefs).toEqual({ version: 1, theme: 'system' });
  });

  it('save merges patch into existing preferences', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ version: 1, theme: 'dark' }));
    const result = await backend.save({ theme: 'light' });
    expect(result).toEqual({ version: 1, theme: 'light' });
  });

  it('save writes result back to localStorage', async () => {
    await backend.save({ theme: 'dark' });
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY)!);
    expect(stored).toEqual({ version: 1, theme: 'dark' });
  });

  it('loadCached returns defaults when localStorage is empty', () => {
    const prefs = backend.loadCached();
    expect(prefs).toEqual({ version: 1, theme: 'system' });
  });

  it('loadCached returns cached preferences synchronously', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ version: 1, theme: 'dark' }));
    const prefs = backend.loadCached();
    expect(prefs).toEqual({ version: 1, theme: 'dark' });
  });

  it('subscribe notifies on cross-tab storage changes', () => {
    const callback = vi.fn();
    const unsubscribe = backend.subscribe(callback);

    const updatedPrefs = { version: 1, theme: 'dark' };
    localStorage.setItem(STORAGE_KEY, JSON.stringify(updatedPrefs));
    window.dispatchEvent(
      new StorageEvent('storage', {
        key: STORAGE_KEY,
        newValue: JSON.stringify(updatedPrefs),
      }),
    );

    expect(callback).toHaveBeenCalledWith(updatedPrefs);

    unsubscribe();

    window.dispatchEvent(
      new StorageEvent('storage', {
        key: STORAGE_KEY,
        newValue: JSON.stringify({ version: 1, theme: 'light' }),
      }),
    );
    expect(callback).toHaveBeenCalledTimes(1);
  });

  it('subscribe ignores storage events for other keys', () => {
    const callback = vi.fn();
    backend.subscribe(callback);

    window.dispatchEvent(
      new StorageEvent('storage', {
        key: 'some-other-key',
        newValue: 'value',
      }),
    );

    expect(callback).not.toHaveBeenCalled();
  });
});

function PreferencesConsumer() {
  const { preferences, updatePreferences } = usePreferences();

  return (
    <div>
      <span data-testid="theme">{preferences.theme}</span>
      <button type="button" onClick={() => updatePreferences({ theme: 'dark' })}>
        set dark
      </button>
    </div>
  );
}

function renderWithProvider() {
  return render(
    <PreferencesProvider>
      <PreferencesConsumer />
    </PreferencesProvider>,
  );
}

describe('PreferencesProvider', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('uses loadCached for initial render', () => {
    vi.spyOn(preferencesBackend, 'loadCached').mockReturnValue({ version: 1, theme: 'dark' });

    renderWithProvider();

    expect(preferencesBackend.loadCached).toHaveBeenCalled();
    expect(screen.getByTestId('theme')).toHaveTextContent('dark');
  });

  it('reconciles with backend on mount', async () => {
    vi.spyOn(preferencesBackend, 'load').mockResolvedValue({ version: 1, theme: 'dark' });

    renderWithProvider();

    await waitFor(() => {
      expect(screen.getByTestId('theme')).toHaveTextContent('dark');
    });
  });

  it('subscribes to cross-tab changes on mount', () => {
    vi.spyOn(preferencesBackend, 'subscribe');

    renderWithProvider();

    expect(preferencesBackend.subscribe).toHaveBeenCalled();
  });

  it('saves to backend when preferences are updated', () => {
    vi.spyOn(preferencesBackend, 'save');

    renderWithProvider();

    act(() => {
      screen.getByRole('button', { name: 'set dark' }).click();
    });

    expect(preferencesBackend.save).toHaveBeenCalledWith({ theme: 'dark' });
  });

  it('updates state optimistically on update', () => {
    renderWithProvider();

    act(() => {
      screen.getByRole('button', { name: 'set dark' }).click();
    });

    expect(screen.getByTestId('theme')).toHaveTextContent('dark');
  });
});
