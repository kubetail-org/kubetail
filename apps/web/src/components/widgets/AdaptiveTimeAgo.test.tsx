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

import { render, screen } from '@testing-library/react';

import { PreferencesProvider } from '@/lib/preferences';

import AdaptiveTimeAgo from './AdaptiveTimeAgo';

const STORAGE_KEY = 'kubetail:preferences';

describe('AdaptiveTimeAgo', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('shows UTC-formatted tooltip by default', () => {
    const date = new Date('2024-06-15T10:30:00Z');
    render(
      <PreferencesProvider>
        <AdaptiveTimeAgo date={date} />
      </PreferencesProvider>,
    );
    const el = screen.getByTitle(/Jun 15, 2024/);
    expect(el.getAttribute('title')).toBe('Jun 15, 2024 10:30:00 (UTC)');
  });

  it('shows tooltip formatted in the selected timezone', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ version: 1, timezone: 'America/New_York' }));

    const date = new Date('2024-06-15T10:30:00Z');
    render(
      <PreferencesProvider>
        <AdaptiveTimeAgo date={date} />
      </PreferencesProvider>,
    );
    const el = screen.getByTitle(/Jun 15, 2024/);
    expect(el.getAttribute('title')).toBe('Jun 15, 2024 06:30:00 (EDT)');
  });
});
