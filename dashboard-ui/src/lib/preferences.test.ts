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

import { createPreferencesBackend } from './preferences';

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
});
