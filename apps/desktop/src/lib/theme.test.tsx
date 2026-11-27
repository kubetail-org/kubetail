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

import { act, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { mockMatchMedia, mockTauriCore, mockTauriEvent } from '@/test-utils';

const { invokeMock, factory: coreFactory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => coreFactory());

const { emitEvent, factory: eventFactory } = mockTauriEvent();
vi.mock('@tauri-apps/api/event', () => eventFactory());

const { ThemeProvider, applyPreference, readInjectedPreference, useColorScheme } = await import('@/lib/theme');

beforeEach(() => {
  document.documentElement.classList.remove('dark');
  invokeMock.mockReset();
  invokeMock.mockResolvedValue(undefined);
});

// The injected global is per-window state; each test sets what it needs.
afterEach(() => {
  delete window.__KUBETAIL_HOST__;
});

describe('readInjectedPreference', () => {
  it('reads the host-injected preference', () => {
    window.__KUBETAIL_HOST__ = { schemaVersion: 1, colorSchemePreference: 'dark' };
    expect(readInjectedPreference()).toBe('dark');
  });

  it('defaults to system when the global is absent', () => {
    expect(readInjectedPreference()).toBe('system');
  });

  it('defaults to system for an unknown value', () => {
    window.__KUBETAIL_HOST__ = { schemaVersion: 1, colorSchemePreference: 'chartreuse' };
    expect(readInjectedPreference()).toBe('system');
  });
});

describe('applyPreference', () => {
  it('adds the dark class for the dark preference', () => {
    mockMatchMedia(false);
    applyPreference('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('removes the dark class for the light preference', () => {
    mockMatchMedia(true);
    document.documentElement.classList.add('dark');
    applyPreference('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('follows the OS for the system preference', () => {
    mockMatchMedia(true);
    applyPreference('system');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });
});

function Harness() {
  const { preference, setPreference } = useColorScheme();
  return (
    <div>
      <span data-testid="preference">{preference}</span>
      <button type="button" onClick={() => setPreference('dark')}>
        dark
      </button>
    </div>
  );
}

function renderProvider() {
  return render(
    <ThemeProvider>
      <Harness />
    </ThemeProvider>,
  );
}

describe('ThemeProvider', () => {
  it('initializes from the host-injected preference', () => {
    mockMatchMedia(false);
    window.__KUBETAIL_HOST__ = { schemaVersion: 1, colorSchemePreference: 'dark' };
    renderProvider();
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(screen.getByTestId('preference').textContent).toBe('dark');
  });

  it('applies an explicit choice and persists it through the host', async () => {
    mockMatchMedia(false);
    renderProvider();
    act(() => screen.getByText('dark').click());
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(screen.getByTestId('preference').textContent).toBe('dark');
    await waitFor(() =>
      expect(invokeMock).toHaveBeenCalledWith('update_host_file', { patch: { colorSchemePreference: 'dark' } }),
    );
  });

  it('hands its readers the same value while the preference stands still', () => {
    mockMatchMedia(false);
    const seen: unknown[] = [];
    function Reader() {
      seen.push(useColorScheme());
      return null;
    }
    const { rerender } = render(
      <ThemeProvider>
        <Reader />
      </ThemeProvider>,
    );
    rerender(
      <ThemeProvider>
        <Reader />
      </ThemeProvider>,
    );
    expect(seen).toHaveLength(2);
    expect(seen[1]).toBe(seen[0]);
  });

  it('follows host-file-updated events (cross-window sync)', () => {
    mockMatchMedia(false);
    renderProvider();
    expect(screen.getByTestId('preference').textContent).toBe('system');
    emitEvent('host-file-updated', { schemaVersion: 1, colorSchemePreference: 'dark' });
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(screen.getByTestId('preference').textContent).toBe('dark');
  });

  it('ignores events with an unknown preference', () => {
    mockMatchMedia(false);
    window.__KUBETAIL_HOST__ = { schemaVersion: 1, colorSchemePreference: 'dark' };
    renderProvider();
    emitEvent('host-file-updated', { schemaVersion: 1, colorSchemePreference: 'chartreuse' });
    expect(screen.getByTestId('preference').textContent).toBe('dark');
  });

  it('re-applies when the OS scheme flips while on system', () => {
    const setDark = mockMatchMedia(false);
    renderProvider();
    expect(document.documentElement.classList.contains('dark')).toBe(false);
    setDark(true);
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('ignores OS changes once an explicit choice is made', () => {
    const setDark = mockMatchMedia(false);
    renderProvider();
    act(() => screen.getByText('dark').click());
    setDark(false);
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });
});
